package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"agentplane/internal/agent"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/worktree"
)

// isolationFlags are the F3.1 options shared by `run` and `serve`. Both are
// optional: they only matter to a graph that sets isolation: worktree.
type isolationFlags struct {
	root   string
	retain string
}

func (f *isolationFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.root, "worktree-root", "", "absolute directory for isolated worktrees (default: the user cache directory)")
	fs.StringVar(&f.retain, "retain-worktrees", string(runtime.RetainFailed), "which isolated worktrees to keep: never, failed or always")
}

// policy validates the flags without touching the filesystem.
func (f isolationFlags) policy() (runtime.RetainPolicy, error) {
	policy := runtime.RetainPolicy(f.retain)
	if policy == "" {
		policy = runtime.RetainFailed
	}
	switch policy {
	case runtime.RetainNever, runtime.RetainFailed, runtime.RetainAlways:
	default:
		return "", fmt.Errorf("--retain-worktrees must be never, failed or always, got %q", f.retain)
	}
	if strings.ContainsAny(f.root, "\x00\n\r") {
		return "", errors.New("--worktree-root contains control characters")
	}
	if f.root != "" && !filepath.IsAbs(f.root) {
		return "", errors.New("--worktree-root must be an absolute path")
	}
	return policy, nil
}

// configure sets the retention policy and, only when the graph asks for
// isolation, the workspace manager. A graph without isolation never reaches Git
// or the worktree root.
func (f isolationFlags) configure(def *graph.Definition, options *runtime.Options) error {
	policy, err := f.policy()
	if err != nil {
		return err
	}
	options.RetainWorktrees = policy
	if usesIsolation(def) {
		options.Workspaces = &gitWorkspaces{root: f.root}
	}
	return nil
}

func usesIsolation(def *graph.Definition) bool {
	for _, n := range def.Nodes {
		if agent.RequestsWorktreeIsolation(n) {
			return true
		}
	}
	return false
}

// gitWorkspaces adapts internal/worktree to the runtime's WorkspaceManager
// port. The manager (which creates the root directory) is built only after a
// source passed preflight, so a rejected run leaves nothing on disk.
type gitWorkspaces struct {
	root string

	mu  sync.Mutex
	mgr *worktree.Manager
}

type gitWorkspace struct{ handle *worktree.Handle }

func (w gitWorkspace) Path() string { return w.handle.Path() }

func (g *gitWorkspaces) Preflight(ctx context.Context, sourceWorkspace string) (runtime.WorkspaceSource, error) {
	root, err := g.resolveRoot()
	if err != nil {
		return runtime.WorkspaceSource{}, err
	}
	src, err := worktree.Preflight(ctx, sourceWorkspace, root)
	if err != nil {
		return runtime.WorkspaceSource{}, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mgr == nil {
		if g.mgr, err = worktree.NewManager(root); err != nil {
			return runtime.WorkspaceSource{}, err
		}
	}
	return runtime.WorkspaceSource{Root: src.Root, BaseSHA: src.BaseSHA}, nil
}

// resolveRoot applies the default, the user cache directory, never a path
// relative to the working directory.
func (g *gitWorkspaces) resolveRoot() (string, error) {
	if g.root != "" {
		return g.root, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no default worktree root (pass --worktree-root): %w", err)
	}
	return filepath.Join(cache, "agentplane", "worktrees"), nil
}

func (g *gitWorkspaces) manager() (*worktree.Manager, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mgr == nil {
		return nil, errors.New("workspace manager used before preflight")
	}
	return g.mgr, nil
}

func (g *gitWorkspaces) Create(ctx context.Context, s runtime.WorkspaceSource, runID, nodeRunID string) (runtime.Workspace, error) {
	m, err := g.manager()
	if err != nil {
		return nil, err
	}
	h, err := m.Create(ctx, worktree.Source{Root: s.Root, BaseSHA: s.BaseSHA}, runID, nodeRunID)
	if err != nil {
		return nil, err
	}
	return gitWorkspace{h}, nil
}

func (g *gitWorkspaces) Snapshot(ctx context.Context, w runtime.Workspace, ref string) (runtime.WorkspaceSnapshot, error) {
	m, err := g.manager()
	if err != nil {
		return runtime.WorkspaceSnapshot{}, err
	}
	gw, ok := w.(gitWorkspace)
	if !ok {
		return runtime.WorkspaceSnapshot{}, errors.New("workspace was not created by this manager")
	}
	res, err := m.Snapshot(ctx, gw.handle, ref)
	return runtime.WorkspaceSnapshot{Commit: res.Commit, ChangedPaths: res.ChangedPaths}, err
}

func (g *gitWorkspaces) Integrate(ctx context.Context, s runtime.WorkspaceSource, tipRef, unitCommit string) (runtime.WorkspaceIntegration, error) {
	m, err := g.manager()
	if err != nil {
		return runtime.WorkspaceIntegration{}, err
	}
	res, err := m.Integrate(ctx, worktree.Source{Root: s.Root, BaseSHA: s.BaseSHA}, tipRef, unitCommit)
	return runtime.WorkspaceIntegration{Conflict: res.Conflict, ConflictPaths: res.ConflictPaths, Omitted: res.Omitted, TipCommit: res.TipCommit, ChangedPaths: res.ChangedPaths}, err
}

func (g *gitWorkspaces) Remove(ctx context.Context, w runtime.Workspace) error {
	m, err := g.manager()
	if err != nil {
		return err
	}
	gw, ok := w.(gitWorkspace)
	if !ok {
		return errors.New("workspace was not created by this manager")
	}
	return m.Remove(ctx, gw.handle)
}

func (*gitWorkspaces) UnitRef(runID, nodeID string) (string, error) {
	return worktree.UnitRef(runID, nodeID)
}

func (*gitWorkspaces) IntegratedRef(runID string) (string, error) {
	return worktree.IntegratedRef(runID)
}
