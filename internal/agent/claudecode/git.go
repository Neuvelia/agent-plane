package claudecode

import (
	"context"
	"path/filepath"

	"agentplane/internal/agent"
)

// Workspaces is the Git workspace evidence port the composition root supplies.
// It keeps this adapter free of any import of the Git adapter (R2).
type Workspaces interface {
	// Canonical resolves the workspace to its non-bare Git worktree root.
	Canonical(ctx context.Context, workspace string) (string, error)
	// Begin records the baseline (snapshot and initially ignored paths) before
	// the child starts.
	Begin(ctx context.Context, root string) (Observation, error)
	// GitDirs returns the absolute, symlink-resolved Git directories of root:
	// the shared (common) one holding config, hooks and info first, then, for a
	// linked worktree whose `.git` is a file, its own per-worktree directory.
	GitDirs(ctx context.Context, root string) ([]string, error)
	// Confined reports whether rel, a slash-separated path under root, is a safe
	// Git path that stays inside the workspace.
	Confined(root, rel string) bool
}

// Observation compares the workspace with the baseline taken by Begin.
type Observation interface {
	// Changed returns the sorted unique workspace-relative paths that differ
	// from the baseline.
	Changed(ctx context.Context) ([]string, error)
}

func (a Adapter) canonicalWorkspace(ctx context.Context, workspace string) (string, error) {
	if a.Workspaces == nil {
		return "", fail("workspace", "no Git workspace evidence is configured", nil)
	}
	root, err := a.Workspaces.Canonical(ctx, workspace)
	if err != nil {
		return "", fail("workspace", "workspace must be a non-bare initialized Git worktree root", err)
	}
	return root, nil
}

func (a Adapter) begin(ctx context.Context, root string) (Observation, error) {
	value, err := a.Workspaces.Begin(ctx, root)
	if err != nil {
		return nil, fail("git", "cannot inspect Git workspace", err)
	}
	return value, nil
}

func changed(ctx context.Context, observation Observation) ([]agent.ChangedFile, error) {
	paths, err := observation.Changed(ctx)
	if err != nil {
		return nil, fail("git", "cannot inspect Git workspace after execution", err)
	}
	files := make([]agent.ChangedFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, agent.ChangedFile{Path: path, Provenance: agent.ProvenanceObserved})
	}
	return files, nil
}

// workspaceRelative returns the slash-separated workspace-relative form of a
// path the model reported, or false when it is not confirmed to lie inside the
// workspace as a safe Git path. This only gates what may be projected as a
// reported file; Git snapshots remain the sole evidence of changes.
func workspaceRelative(workspaces Workspaces, root, reported string) (string, bool) {
	path := reported
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if !within(root, path) {
		return "", false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if workspaces == nil || !workspaces.Confined(root, rel) {
		return "", false
	}
	return rel, true
}
