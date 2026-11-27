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

// guardedFiles are the Git control files that Git snapshots exclude but that
// change what Git reports or runs: `config` and `config.worktree` (including
// `core.hooksPath` and `core.excludesFile`), `info/exclude` (which can hide a new
// file from the untracked listing) and `info/attributes`. Every file under
// `hooks/` is guarded too. The index is not guarded: Git and the CLI may refresh
// its stat cache legitimately.
var guardedFiles = []string{"config", "config.worktree", "info/exclude", "info/attributes"}

// gitGuard fingerprints the guarded Git control files of every Git directory
// the workspace uses: the shared (common) one and, for a linked worktree, its
// own. A file tool that writes there is invisible to the changed-file evidence,
// so any difference between the fingerprints taken before and after the run
// fails closed.
type gitGuard struct {
	dirs   []string
	before map[string]string
}

// newGitGuard resolves the Git directories through the Workspaces port and
// records the baseline fingerprints.
func (a Adapter) newGitGuard(ctx context.Context, root string) (*gitGuard, error) {
	dirs, err := a.Workspaces.GitDirs(ctx, root)
	if err != nil || len(dirs) == 0 || !allAbsolute(dirs) {
		return nil, fail("git", "cannot resolve the Git directory (Git 2.5 or later is required)", err)
	}
	before, err := fingerprintAll(dirs)
	if err != nil {
		return nil, fail("git", "cannot fingerprint Git control files", err)
	}
	return &gitGuard{dirs: dirs, before: before}, nil
}

// verify fails with claude_git when any guarded file was added, changed,
// removed or had its mode changed. The message names no path.
func (g *gitGuard) verify() error {
	after, err := fingerprintAll(g.dirs)
	if err != nil {
		return fail("git", "cannot fingerprint Git control files after execution", err)
	}
	if !maps.Equal(g.before, after) {
		return fail("git", "Git configuration, exclude rules, hooks or attributes changed during execution; output rejected", nil)
	}
	return nil
}

func allAbsolute(paths []string) bool {
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return false
		}
	}
	return true
}

// fingerprintAll fingerprints each directory under a key prefix naming its
// position, so the same relative file in two directories stays distinct.
func fingerprintAll(dirs []string) (map[string]string, error) {
	sums := map[string]string{}
	for i, dir := range dirs {
		dirSums, err := fingerprint(dir)
		if err != nil {
			return nil, err
		}
		for rel, sum := range dirSums {
			sums[fmt.Sprintf("%d:%s", i, rel)] = sum
		}
	}
	return sums, nil
}

// fingerprint maps each guarded file, relative to dir, to a digest of its
// presence, mode and content.
func fingerprint(dir string) (map[string]string, error) {
	sums := map[string]string{}
	for _, rel := range append(guardedFiles, "hooks") {
		sum, err := fileSum(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		sums[rel] = sum
	}
	if err := fingerprintHooks(filepath.Join(dir, "hooks"), sums); err != nil {
		return nil, err
	}
	return sums, nil
}

// fingerprintHooks adds every file below hooks. A symlinked hooks directory is
// followed (its link text is already guarded as "hooks"), so the files it
// points at are guarded too.
func fingerprintHooks(hooks string, sums map[string]string) error {
	resolved, err := filepath.EvalSymlinks(hooks)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		sum, err := fileSum(path)
		sums["hooks/"+filepath.ToSlash(rel)] = sum
		return err
	})
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
