package store

import (
	"errors"
	"fmt"
	"os"
)

const lockFile = "run.lock"

var (
	// ErrRunLocked means another writer already holds the run's lock.
	ErrRunLocked = errors.New("run is locked by another writer; a run directory has exactly one writer")
	// ErrLockUnsupported is returned on platforms without an advisory file lock
	// implementation, so a missing lock is an explicit build-time decision.
	ErrLockUnsupported = errors.New("run locks are not supported on this platform")
)

// runLock is the exclusive advisory lock a writer holds on <run>/run.lock for
// as long as it may still append. The operating system releases it when the
// process dies, which is how a reader tells a live run from an interrupted one.
// The lock file is never read or written, so byte-range locking on Windows
// cannot block readers of events.jsonl.
type runLock struct{ f *os.File }

// acquireRunLock creates path (0600, empty) if needed and takes the exclusive
// lock. With wait it blocks instead of failing on a conflict: the writer owns a
// freshly created run directory, so the only possible holder is a reader's
// momentary shared probe, which must never make a writer fail to start.
func acquireRunLock(path string, wait bool) (*runLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("create run lock: %w", err)
	}
	held, err := tryLock(f, true, wait)
	if err == nil && held {
		return &runLock{f: f}, nil
	}
	if cerr := f.Close(); cerr != nil {
		err = errors.Join(err, cerr)
	}
	if err != nil {
		return nil, fmt.Errorf("lock run: %w", err)
	}
	return nil, ErrRunLocked
}

// release unlocks and closes the lock file. It is idempotent.
func (l *runLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	f := l.f
	l.f = nil
	err := unlock(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// runLockHeld reports whether a live writer holds path. It opens the file
// read-only and never creates it: a missing file means no live writer (format 0
// runs, or runs from a binary older than the lock).
func runLockHeld(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open run lock: %w", err)
	}
	acquired, err := tryLock(f, false, false)
	if err == nil && acquired {
		err = unlock(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, fmt.Errorf("probe run lock: %w", err)
	}
	return !acquired, nil
}
