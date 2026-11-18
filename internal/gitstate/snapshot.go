package gitstate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type fileState struct{ Index, Working string }
type gitSnapshot map[string]fileState

// Snapshot is a provider-neutral immutable observation of index and worktree state.
type Snapshot = gitSnapshot

// Baseline is an in-memory snapshot taken before a unit begins. Unlike
// adapter metadata snapshots, it includes ignored files so scope checks
// cannot miss changes beneath an existing ignore rule.
type Baseline struct {
	Root, Head string
	files      gitSnapshot
}
type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}
type inspectionError struct {
	message string
	cause   error
}

func (e *inspectionError) Error() string { return "git scope: " + e.message }
func (e *inspectionError) Unwrap() error { return e.cause }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	room := b.limit - b.Len()
	if len(p) > room {
		b.overflow = true
		p = p[:room]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func fail(_ string, message string, cause error) error {
	return &inspectionError{message: message, cause: cause}
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	result, err := Run(ctx, dir, RunOptions{}, args...)
	if err != nil {
		var runErr *RunError
		if errors.As(err, &runErr) && runErr.Class == RunOutputLimit {
			return nil, fail("git", "Git workspace listing exceeds 16 MiB", nil)
		}
		return nil, fail("git", "inspect initialized Git workspace with HEAD", err)
	}
	return result.Stdout, nil
}
func CanonicalWorkspace(ctx context.Context, workspace string) (string, error) {
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
	return snapshotAfter(ctx, dir, nil, nil, false)
}

// Ignored contents stay unread. Directory prefixes make unknown initial state
// explicit without enumerating or hashing the contents of ignored trees.
func ignoredInventory(ctx context.Context, dir string) (map[string]bool, error) {
	data, err := git(ctx, dir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, raw := range bytes.Split(data, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := string(raw)
		if _, err := ConfinedPath(dir, strings.TrimSuffix(path, "/")); err != nil {
			return nil, err
		}
		paths[path] = true
	}
	return paths, nil
}

func snapshotAfter(ctx context.Context, dir string, baseline gitSnapshot, ignored map[string]bool, includeIgnored bool) (gitSnapshot, error) {
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
	otherArgs := []string{"ls-files", "--others", "-z"}
	if !includeIgnored {
		otherArgs = []string{"ls-files", "--others", "--exclude-standard", "-z"}
	}
	untracked, err := git(ctx, dir, otherArgs...)
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
	// Adapter metadata excludes ignored contents. If ignore rules expose a
	// previously ignored path, its initial contents are unknown there.
	for path := range result {
		if includeIgnored {
			continue
		}
		if _, known := baseline[path]; known {
			continue
		}
		unknown := ignored[path]
		for ancestor := path; !unknown; {
			separator := strings.LastIndexByte(ancestor, '/')
			if separator < 0 {
				break
			}
			ancestor = ancestor[:separator]
			unknown = ignored[ancestor+"/"]
		}
		if unknown {
			return nil, fail("git", "newly exposed ignored path has unknown baseline; set ignore rules before execution and rerun", nil)
		}
	}
	// Re-observe baseline files even if Git no longer lists them. Keep current
	// index state when present; absence here means no after-index entry.
	for path := range baseline {
		if _, present := result[path]; !present {
			result[path] = fileState{}
		}
	}
	for path, state := range result {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		full, err := ConfinedPath(dir, path)
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
func ConfinedPath(root, path string) (string, error) {
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
func changed(before, after gitSnapshot) []string {
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
	return sorted
}

// Capture records HEAD identity and exact index/working-file state, including dirt.
func Capture(ctx context.Context, workspace string) (Baseline, error) {
	if ctx == nil {
		return Baseline{}, fmt.Errorf("Git capture context is nil")
	}
	root, err := CanonicalWorkspace(ctx, workspace)
	if err != nil {
		return Baseline{}, err
	}
	head, err := git(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Baseline{}, err
	}
	files, err := snapshotAfter(ctx, root, nil, nil, true)
	if err != nil {
		return Baseline{}, err
	}
	return Baseline{Root: root, Head: strings.TrimSpace(string(head)), files: files}, nil
}

// Changed compares current Git-observed state with the captured pre-unit state.
func Changed(ctx context.Context, baseline Baseline) ([]string, error) {
	if ctx == nil {
		return nil, fmt.Errorf("Git inspection context is nil")
	}
	if baseline.Root == "" || baseline.files == nil {
		return nil, fmt.Errorf("invalid Git baseline")
	}
	root, err := CanonicalWorkspace(ctx, baseline.Root)
	if err != nil {
		return nil, err
	}
	if root != baseline.Root {
		return nil, fmt.Errorf("Git baseline workspace changed")
	}
	after, err := snapshotAfter(ctx, root, baseline.files, nil, true)
	if err != nil {
		return nil, err
	}
	return changed(baseline.files, after), nil
}

func SnapshotNow(ctx context.Context, root string) (Snapshot, error) { return snapshot(ctx, root) }
func SnapshotAfter(ctx context.Context, root string, before Snapshot, ignored map[string]bool) (Snapshot, error) {
	return snapshotAfter(ctx, root, before, ignored, false)
}
func IgnoredInventory(ctx context.Context, root string) (map[string]bool, error) {
	return ignoredInventory(ctx, root)
}
func ChangedSnapshots(before, after Snapshot) []string { return changed(before, after) }
