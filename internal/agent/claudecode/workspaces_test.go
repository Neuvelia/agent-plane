package claudecode

import (
	"context"
	"path/filepath"
	"slices"
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

// GitDirs resolves the shared Git directory (config, hooks, info) of root and,
// for a linked worktree whose .git is a file, its per-worktree directory.
// `--path-format=absolute` needs Git 2.31, so relative output is resolved
// against root here instead (rev-parse runs in root).
func (testWorkspaces) GitDirs(ctx context.Context, root string) ([]string, error) {
	result, err := gitstate.Run(ctx, root, gitstate.RunOptions{}, "rev-parse", "--git-common-dir", "--git-dir")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, line := range strings.Split(strings.TrimSuffix(string(result.Stdout), "\n"), "\n") {
		if !filepath.IsAbs(line) {
			line = filepath.Join(root, line)
		}
		dir, err := filepath.EvalSymlinks(line)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
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
