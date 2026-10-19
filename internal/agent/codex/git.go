package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"agentplane/internal/agent"
)

type fileState struct{ Index, Working string }
type gitSnapshot map[string]fileState

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/git", argv...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	var b limitedBuffer
	b.limit = 16 << 20
	cmd.Stdout = &b
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err(), context.Cause(ctx))
		}
		return nil, fail("git", "inspect initialized Git workspace with HEAD", err)
	}
	if b.overflow {
		return nil, fail("git", "Git workspace listing exceeds 16 MiB", nil)
	}
	return b.Bytes(), nil
}
func canonicalWorkspace(ctx context.Context, workspace string) (string, error) {
	if !filepath.IsAbs(workspace) {
		return "", fail("workspace", "workspace must be absolute", nil)
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fail("workspace", "workspace must exist and be accessible", err)
	}
	top, err := git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fail("workspace", "workspace must be a non-bare Git worktree root", err)
	}
	canonical, err := filepath.EvalSymlinks(strings.TrimSuffix(string(top), "\n"))
	if err != nil || canonical != root {
		return "", fail("workspace", "workspace must equal the Git worktree root", err)
	}
	if _, err = git(ctx, root, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", fail("workspace", "workspace requires a committed HEAD", err)
	}
	return root, nil
}
func snapshot(ctx context.Context, dir string) (gitSnapshot, error) {
	entries, err := git(ctx, dir, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	result := gitSnapshot{}
	for _, entry := range bytes.Split(entries, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		header, path, ok := strings.Cut(string(entry), "\t")
		parts := strings.Fields(header)
		if !ok || len(parts) != 3 || parts[2] != "0" || parts[0] == "160000" {
			return nil, fail("git", "unresolved index stages or submodules are unsupported", nil)
		}
		result[path] = fileState{Index: header}
	}
	untracked, err := git(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, path := range bytes.Split(untracked, []byte{0}) {
		if len(path) > 0 {
			if _, ok := result[string(path)]; !ok {
				result[string(path)] = fileState{}
			}
		}
	}
	for path, state := range result {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		full, err := confinedPath(dir, path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(full)
		if errors.Is(err, os.ErrNotExist) {
			result[path] = state
			continue
		}
		if err != nil {
			return nil, fail("git", "cannot inspect workspace file", err)
		}
		var digest [32]byte
		switch {
		case info.Mode().IsRegular():
			f, e := os.Open(full)
			if e != nil {
				return nil, fail("git", "cannot read workspace file", e)
			}
			h := sha256.New()
			_, e = io.Copy(h, f)
			closeErr := f.Close()
			if e != nil || closeErr != nil {
				return nil, fail("git", "cannot hash workspace file", errors.Join(e, closeErr))
			}
			copy(digest[:], h.Sum(nil))
		case info.Mode()&os.ModeSymlink != 0:
			target, e := os.Readlink(full)
			if e != nil {
				return nil, fail("git", "cannot read workspace symlink", e)
			}
			digest = sha256.Sum256([]byte(target))
		default:
			return nil, fail("git", "unsupported filesystem entry in workspace", nil)
		}
		state.Working = info.Mode().String() + ":" + hex.EncodeToString(digest[:])
		result[path] = state
	}
	return result, nil
}
func confinedPath(root, path string) (string, error) {
	if !utf8.ValidString(path) || path == "" || filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", fail("git", "invalid Git path", nil)
	}
	components := strings.Split(path, "/")
	for _, p := range components {
		if p == "" || p == "." || p == ".." || strings.EqualFold(p, ".git") {
			return "", fail("git", "unsafe Git path", nil)
		}
	}
	parent := root
	for _, p := range components[:len(components)-1] {
		parent = filepath.Join(parent, p)
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fail("git", "Git path ancestor escapes workspace or is unsupported", err)
		}
	}
	return filepath.Join(root, filepath.FromSlash(path)), nil
}
func changed(before, after gitSnapshot) []agent.ChangedFile {
	paths := map[string]bool{}
	for p, s := range before {
		if a, ok := after[p]; !ok || a != s {
			paths[p] = true
		}
	}
	for p, s := range after {
		if b, ok := before[p]; !ok || b != s {
			paths[p] = true
		}
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	files := make([]agent.ChangedFile, 0, len(paths))
	for _, p := range sorted {
		files = append(files, agent.ChangedFile{Path: p, Provenance: agent.ProvenanceObserved})
	}
	return files
}
