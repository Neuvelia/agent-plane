package codex

import (
	"context"

	"agentplane/internal/agent"
	"agentplane/internal/gitstate"
)

type gitSnapshot = gitstate.Snapshot

func canonicalWorkspace(ctx context.Context, workspace string) (string, error) {
	root, err := gitstate.CanonicalWorkspace(ctx, workspace)
	if err != nil {
		return "", fail("workspace", "workspace must be a non-bare initialized Git worktree root", err)
	}
	return root, nil
}
func snapshot(ctx context.Context, root string) (gitSnapshot, error) {
	value, err := gitstate.SnapshotNow(ctx, root)
	if err != nil {
		return nil, fail("git", "cannot inspect Git workspace", err)
	}
	return value, nil
}
func snapshotAfter(ctx context.Context, root string, baseline gitSnapshot, ignored map[string]bool) (gitSnapshot, error) {
	value, err := gitstate.SnapshotAfter(ctx, root, baseline, ignored)
	if err != nil {
		return nil, fail("git", "cannot inspect Git workspace after execution", err)
	}
	return value, nil
}
func ignoredInventory(ctx context.Context, root string) (map[string]bool, error) {
	value, err := gitstate.IgnoredInventory(ctx, root)
	if err != nil {
		return nil, fail("git", "cannot inspect ignored Git paths", err)
	}
	return value, nil
}
func confinedPath(root, path string) (string, error) {
	value, err := gitstate.ConfinedPath(root, path)
	if err != nil {
		return "", fail("git", "unsafe Git path", err)
	}
	return value, nil
}
func changed(before, after gitSnapshot) []agent.ChangedFile {
	paths := gitstate.ChangedSnapshots(before, after)
	files := make([]agent.ChangedFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, agent.ChangedFile{Path: path, Provenance: agent.ProvenanceObserved})
	}
	return files
}
