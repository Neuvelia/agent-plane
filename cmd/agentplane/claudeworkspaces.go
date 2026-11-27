package main

import (
	"context"
	"path/filepath"
	"strings"

	"agentplane/internal/agent/claudecode"
	"agentplane/internal/gitstate"
)

// claudeWorkspaces backs the Claude Code adapter's Git evidence port with
// internal/gitstate. It lives in the composition root so the adapter imports no
// other adapter (R2, R7).
type claudeWorkspaces struct{}

func (claudeWorkspaces) Canonical(ctx context.Context, workspace string) (string, error) {
	return gitstate.CanonicalWorkspace(ctx, workspace)
}

func (claudeWorkspaces) Begin(ctx context.Context, root string) (claudecode.Observation, error) {
	before, err := gitstate.SnapshotNow(ctx, root)
	if err != nil {
		return nil, err
	}
	ignored, err := gitstate.IgnoredInventory(ctx, root)
	if err != nil {
		return nil, err
	}
	return claudeObservation{root: root, before: before, ignored: ignored}, nil
}

// GitDir resolves the shared Git directory (config, hooks, info) of root,
// including for a linked worktree whose .git is a file.
func (claudeWorkspaces) GitDir(ctx context.Context, root string) (string, error) {
	result, err := gitstate.Run(ctx, root, gitstate.RunOptions{}, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(strings.TrimSuffix(string(result.Stdout), "\n"))
}

func (claudeWorkspaces) Confined(root, rel string) bool {
	_, err := gitstate.ConfinedPath(root, rel)
	return err == nil
}

type claudeObservation struct {
	root    string
	before  gitstate.Snapshot
	ignored map[string]bool
}

func (o claudeObservation) Changed(ctx context.Context) ([]string, error) {
	after, err := gitstate.SnapshotAfter(ctx, o.root, o.before, o.ignored)
	if err != nil {
		return nil, err
	}
	return gitstate.ChangedSnapshots(o.before, after), nil
}

// newClaudeAdapter builds the installed Claude Code adapter for binary.
func newClaudeAdapter(binary string) claudecode.Adapter {
	return claudecode.Adapter{Binary: binary, Workspaces: claudeWorkspaces{}}
}
