package attemptdiff

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// scratchLimit bounds the bytes one run keeps on disk (Q6, owner decision
// 2026-11-18). Beyond it a file is listed with hashes and reason budget.
const defaultScratchLimit = 64 << 20

var errScratchBudget = errors.New("scratch budget exhausted")

// scratch holds before-content that Git cannot give back: dirty or untracked
// text files. The directory is private (0700), files are 0600 and keyed by
// SHA-256, so concurrent writers of the same content converge.
type scratch struct {
	dir   string
	limit int64
	mu    sync.Mutex
	used  int64
}

func openScratch() (*scratch, error) {
	dir, err := os.MkdirTemp("", "agentplane-diff-")
	if err != nil {
		return nil, err
	}
	return &scratch{dir: dir, limit: defaultScratchLimit}, nil
}

func (s *scratch) path(sha string) string { return filepath.Join(s.dir, sha) }

func (s *scratch) put(sha string, data []byte) error {
	if _, err := os.Lstat(s.path(sha)); err == nil {
		return nil
	}
	if !s.reserve(int64(len(data))) {
		return errScratchBudget
	}
	tmp, err := os.CreateTemp(s.dir, "tmp-")
	if err != nil {
		return err
	}
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(0o600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path(sha))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

func (s *scratch) reserve(n int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used+n > s.limit {
		return false
	}
	s.used += n
	return true
}

func (s *scratch) get(sha string) ([]byte, error) { return os.ReadFile(s.path(sha)) }

func (s *scratch) remove() error { return os.RemoveAll(s.dir) }
