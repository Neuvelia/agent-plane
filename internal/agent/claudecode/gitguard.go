package claudecode

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
)

// gitGuard fingerprints the Git control files that Git snapshots exclude but a
// later plain `git` command would act on: `config`, `info/attributes` and every
// file under `hooks/` of the real (common) Git directory. A file tool that
// writes there is invisible to the changed-file evidence, so any difference
// between the fingerprints taken before and after the run fails closed.
type gitGuard struct {
	dir    string
	before map[string]string
}

// newGitGuard resolves the real Git directory (which also covers a linked
// worktree whose `.git` is a file) through the Workspaces port and records the
// baseline fingerprints.
func (a Adapter) newGitGuard(ctx context.Context, root string) (*gitGuard, error) {
	dir, err := a.Workspaces.GitDir(ctx, root)
	if err != nil || !filepath.IsAbs(dir) {
		return nil, fail("git", "cannot resolve the Git directory", err)
	}
	before, err := fingerprint(dir)
	if err != nil {
		return nil, fail("git", "cannot fingerprint Git control files", err)
	}
	return &gitGuard{dir: dir, before: before}, nil
}

// verify fails with claude_git when any guarded file was added, changed,
// removed or had its mode changed. The message names no path.
func (g *gitGuard) verify() error {
	after, err := fingerprint(g.dir)
	if err != nil {
		return fail("git", "cannot fingerprint Git control files after execution", err)
	}
	if !maps.Equal(g.before, after) {
		return fail("git", "Git configuration, hooks or attributes changed during execution; output rejected", nil)
	}
	return nil
}

// fingerprint maps each guarded file, relative to dir, to a digest of its
// presence, mode and content.
func fingerprint(dir string) (map[string]string, error) {
	sums := map[string]string{}
	for _, rel := range []string{"config", "info/attributes"} {
		sum, err := fileSum(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		sums[rel] = sum
	}
	hooks := filepath.Join(dir, "hooks")
	err := filepath.WalkDir(hooks, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum, err := fileSum(path)
		sums[filepath.ToSlash(rel)] = sum
		return err
	})
	if err != nil {
		return nil, err
	}
	return sums, nil
}

// fileSum digests presence, mode and content; a symlink contributes its target
// text and never its destination.
func fileSum(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	var content []byte
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, linkErr := os.Readlink(path)
		content, err = []byte(target), linkErr
	case info.Mode().IsRegular():
		content, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%x", info.Mode(), sha256.Sum256(content)), nil
}
