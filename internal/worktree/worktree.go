package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agentplane/internal/gitstate"
)

const (
	// Checking out a large repository or staging a large worktree can take far
	// longer than the five seconds used for read-only inspection.
	mutateTimeout = 10 * time.Minute
	// Commits are created from a fixed identity so no user configuration is read.
	commitIdentity = "agentplane"
	commitEmail    = "agentplane@localhost.invalid"
)

var identityArgs = []string{"-c", "user.name=" + commitIdentity, "-c", "user.email=" + commitEmail, "-c", "commit.gpgsign=false"}

// Manager owns a worktree root directory and the handles created in it. It
// removes only handles it created itself.
type Manager struct {
	root string // canonical, mode 0700, outside any source repository

	mu   sync.Mutex
	live map[*Handle]struct{}
}

// Handle is one isolated worktree. Its path is derived only from validated
// run and node-run identifiers.
type Handle struct {
	path      string
	src       Source
	runID     string
	nodeRunID string
}

// Path returns the absolute canonical worktree directory.
func (h *Handle) Path() string { return h.path }

// NewManager creates (or validates) the worktree root. A new root is created
// with mode 0700; an existing one must be a real directory, not a symlink, and
// not accessible to group or others.
func NewManager(root string) (*Manager, error) {
	if !filepath.IsAbs(root) || strings.ContainsAny(root, "\x00\n\r") {
		return nil, fail(CodeWorktreeRoot, "worktree root must be an absolute path", nil)
	}
	root = filepath.Clean(root)
	if err := ensureRoot(root); err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fail(CodeWorktreeRoot, "cannot resolve the worktree root", err)
	}
	return &Manager{root: real, live: map[*Handle]struct{}{}}, nil
}

// ensureRoot creates a missing root with mode 0700 and rejects a root that is
// not a private, real directory.
func ensureRoot(root string) error {
	info, err := os.Lstat(root)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(root, 0o700); err != nil {
			return fail(CodeWorktreeRoot, "cannot create the worktree root", err)
		}
		if info, err = os.Lstat(root); err != nil {
			return fail(CodeWorktreeRoot, "cannot inspect the worktree root", err)
		}
	case err != nil:
		return fail(CodeWorktreeRoot, "cannot inspect the worktree root", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fail(CodeWorktreeRoot, "worktree root must be a real directory, not a symlink or file", nil)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fail(CodeWorktreeRoot, fmt.Sprintf("worktree root permissions %o expose it to other users; run chmod 700 on it", info.Mode().Perm()), nil)
	}
	return nil
}

// Root returns the canonical worktree root.
func (m *Manager) Root() string { return m.root }

// Create adds a detached linked worktree at src.BaseSHA. Preflight must have
// accepted the source first: Create trusts that no filter driver is configured.
func (m *Manager) Create(ctx context.Context, src Source, runID, nodeRunID string) (*Handle, error) {
	if err := m.validateCreate(src, runID, nodeRunID); err != nil {
		return nil, err
	}
	path := filepath.Join(m.root, "agentplane-"+runID+"-"+nodeRunID)
	if err := requireAbsent(path); err != nil {
		return nil, err
	}
	if _, err := gitstate.Run(ctx, src.Root, gitstate.RunOptions{Timeout: mutateTimeout}, "worktree", "add", "--quiet", "--detach", path, src.BaseSHA); err != nil {
		return nil, fail(CodeCreate, "git could not create the worktree (check free disk space and that the base commit still exists)", err)
	}
	if canon, err := gitstate.CanonicalWorkspace(ctx, path); err != nil || canon != path {
		return nil, fail(CodeCreate, "the new worktree is not a usable Git worktree root", err)
	}
	h := &Handle{path: path, src: src, runID: runID, nodeRunID: nodeRunID}
	m.mu.Lock()
	m.live[h] = struct{}{}
	m.mu.Unlock()
	return h, nil
}

func (m *Manager) validateCreate(src Source, runID, nodeRunID string) error {
	if err := validateID("run id", runID); err != nil {
		return fail(CodeInvalidInput, err.Error(), nil)
	}
	if err := validateID("node run id", nodeRunID); err != nil {
		return fail(CodeInvalidInput, err.Error(), nil)
	}
	if !shaPattern.MatchString(src.BaseSHA) || !filepath.IsAbs(src.Root) {
		return fail(CodeInvalidInput, "source was not produced by Preflight", nil)
	}
	if within(src.Root, m.root) {
		return fail(CodeWorktreeRoot, "worktree root must not be inside the source workspace", nil)
	}
	return nil
}

// requireAbsent refuses to reuse an existing worktree directory.
func requireAbsent(path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return fail(CodeCreate, "the worktree directory already exists; refusing to reuse it", nil)
	}
	if !os.IsNotExist(err) {
		return fail(CodeCreate, "cannot inspect the worktree directory", err)
	}
	return nil
}

// SnapshotResult describes the commit recorded for a unit.
type SnapshotResult struct {
	Commit       string
	ChangedPaths int // paths that differ from the base commit
}

// Snapshot records the worktree's current contents as a commit whose only
// parent is the base commit and stores it at ref. Ignored files are excluded.
// It never moves a branch and never touches the source index or working tree.
func (m *Manager) Snapshot(ctx context.Context, h *Handle, ref string) (SnapshotResult, error) {
	if err := m.requireLive(h); err != nil {
		return SnapshotResult{}, err
	}
	if err := validateRef(ref); err != nil {
		return SnapshotResult{}, fail(CodeInvalidInput, err.Error(), nil)
	}
	opts := gitstate.RunOptions{Timeout: mutateTimeout}
	if _, err := gitstate.Run(ctx, h.path, opts, "add", "--all"); err != nil {
		return SnapshotResult{}, fail(CodeSnapshot, "git could not stage the worktree contents", err)
	}
	staged, err := gitstate.Run(ctx, h.path, opts, "ls-files", "--stage", "-z")
	if err != nil {
		return SnapshotResult{}, fail(CodeSnapshot, "git could not list the worktree contents", err)
	}
	if hasGitlink(staged.Stdout) {
		return SnapshotResult{}, fail(CodeNestedRepo, "the unit's worktree contains a nested Git repository or submodule; remove it before the unit can be integrated", nil)
	}
	tree, err := gitstate.Run(ctx, h.path, opts, "write-tree")
	if err != nil {
		return SnapshotResult{}, fail(CodeSnapshot, "git could not write the worktree tree", err)
	}
	commit, err := commitTree(ctx, h.path, strings.TrimSpace(string(tree.Stdout)), "agentplane unit snapshot", h.src.BaseSHA)
	if err != nil {
		return SnapshotResult{}, err
	}
	if _, err := gitstate.Run(ctx, h.src.Root, opts, "update-ref", ref, commit); err != nil {
		return SnapshotResult{}, fail(CodeSnapshot, "git could not record the unit ref", err)
	}
	changed, err := countChanged(ctx, h.src.Root, h.src.BaseSHA, commit)
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{Commit: commit, ChangedPaths: changed}, nil
}

// commitTree writes a commit object with the fixed identity and parents. It
// creates no ref.
func commitTree(ctx context.Context, dir, tree, message string, parents ...string) (string, error) {
	if !shaPattern.MatchString(tree) {
		return "", fail(CodeSnapshot, "git returned an unexpected tree id", nil)
	}
	args := append([]string{}, identityArgs...)
	args = append(args, "commit-tree", tree)
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-m", message)
	res, err := gitstate.Run(ctx, dir, gitstate.RunOptions{Timeout: mutateTimeout}, args...)
	if err != nil {
		return "", fail(CodeSnapshot, "git could not create the commit", err)
	}
	commit := strings.TrimSpace(string(res.Stdout))
	if !shaPattern.MatchString(commit) {
		return "", fail(CodeSnapshot, "git returned an unexpected commit id", nil)
	}
	return commit, nil
}

// countChanged counts the paths that differ between two commits.
func countChanged(ctx context.Context, dir, from, to string) (int, error) {
	res, err := gitstate.Run(ctx, dir, gitstate.RunOptions{Timeout: mutateTimeout}, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", from, to)
	if err != nil {
		return 0, fail(CodeSnapshot, "git could not compare commits", err)
	}
	n := 0
	for _, entry := range strings.Split(string(res.Stdout), "\x00") {
		if entry != "" {
			n++
		}
	}
	return n, nil
}

// Remove deletes a worktree this manager created, including untracked and
// ignored files inside it. It never accepts a path, so a graph-controlled
// string can never select what is deleted. On failure the directory is left in
// place and stays tracked, so the caller can report its path and retry.
func (m *Manager) Remove(ctx context.Context, h *Handle) error {
	if err := m.requireLive(h); err != nil {
		return err
	}
	if _, err := gitstate.Run(ctx, h.src.Root, gitstate.RunOptions{Timeout: mutateTimeout}, "worktree", "remove", "--force", h.path); err != nil {
		return fail(CodeRemove, "git could not remove the worktree; delete the directory manually", err)
	}
	m.mu.Lock()
	delete(m.live, h)
	m.mu.Unlock()
	return nil
}

func (m *Manager) requireLive(h *Handle) error {
	if h == nil {
		return fail(CodeInvalidInput, "worktree handle is nil", nil)
	}
	m.mu.Lock()
	_, ok := m.live[h]
	m.mu.Unlock()
	if !ok {
		return fail(CodeInvalidInput, "worktree handle was not created by this manager or was already removed", nil)
	}
	return nil
}
