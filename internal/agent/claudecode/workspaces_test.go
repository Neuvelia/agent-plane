package claudecode

import (
	"context"
	"path/filepath"
	"strings"

	"agentplane/internal/gitstate"
)

// testWorkspaces is the test-side implementation of the Git evidence port that
// cmd/agentplane supplies in production. Test imports are not subject to the
// production layer rules.
type testWorkspaces struct{}

func (testWorkspaces) Canonical(ctx context.Context, workspace string) (string, error) {
	return gitstate.CanonicalWorkspace(ctx, workspace)
}

func (testWorkspaces) Begin(ctx context.Context, root string) (Observation, error) {
	before, err := gitstate.SnapshotNow(ctx, root)
	if err != nil {
		return nil, err
	}
	ignored, err := gitstate.IgnoredInventory(ctx, root)
	if err != nil {
		return nil, err
	}
	return testObservation{root: root, before: before, ignored: ignored}, nil
}

// GitDir resolves the shared Git directory (config, hooks, info) of root,
// including for a linked worktree whose .git is a file.
func (testWorkspaces) GitDir(ctx context.Context, root string) (string, error) {
	result, err := gitstate.Run(ctx, root, gitstate.RunOptions{}, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(strings.TrimSuffix(string(result.Stdout), "\n"))
}

func (testWorkspaces) Confined(root, rel string) bool {
	_, err := gitstate.ConfinedPath(root, rel)
	return err == nil
}

type testObservation struct {
	root    string
	before  gitstate.Snapshot
	ignored map[string]bool
}

func (o testObservation) Changed(ctx context.Context) ([]string, error) {
	after, err := gitstate.SnapshotAfter(ctx, o.root, o.before, o.ignored)
	if err != nil {
		return nil, err
	}
	return gitstate.ChangedSnapshots(o.before, after), nil
}
