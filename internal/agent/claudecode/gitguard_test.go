package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentplane/internal/agent"
)

// failingGitDir is a Workspaces whose Git directory cannot be resolved.
type failingGitDir struct{ testWorkspaces }

func (failingGitDir) GitDir(context.Context, string) (string, error) {
	return "", errors.New("no git dir")
}

func executeFake(t *testing.T, workspace, body string, refs ...string) (agent.AgentExecutionOutput, error) {
	t.Helper()
	return Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, body)}.Execute(context.Background(), agent.AgentExecutionInput{Workspace: workspace, Task: "t", EnvironmentRefs: refs})
}

func requireKind(t *testing.T, err error, kind agent.ErrorKind) {
	t.Helper()
	var typed *agent.ExecutionError
	if !errors.As(err, &typed) || typed.Kind != kind {
		t.Fatalf("error=%v, want kind %s", err, kind)
	}
	assertNoPrivate(t, err.Error())
}

// roadmap: F1.2b-AC3
func TestGitControlFileWritesAreRejected(t *testing.T) {
	cases := map[string]string{
		"new hook":        "printf '#!/bin/sh\necho PRIVATE_HOOK\n' >.git/hooks/pre-commit\n",
		"changed hook":    "printf 'changed' >>.git/hooks/pre-commit.sample\n",
		"deleted hook":    "rm .git/hooks/pre-commit.sample\n",
		"config":          "printf '[core]\n\thooksPath = x\n' >>.git/config\n",
		"attributes":      "mkdir -p .git/info\nprintf '* filter=x\n' >.git/info/attributes\n",
		"hook mode":       "chmod 0700 .git/hooks/pre-commit.sample\n",
		"config deletion": "rm .git/config\n",
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := executeFake(t, repo(t), edit+emit(recResult))
			requireKind(t, err, "claude_git")
			if out.Summary != "" || out.ChangedFiles != nil || out.Artifacts != nil {
				t.Fatalf("output accepted: %+v", out)
			}
		})
	}
}

func TestGitControlFilesUnchangedRunSucceeds(t *testing.T) {
	d := repo(t)
	out, err := executeFake(t, d, "printf edited >file.txt\n"+emit(recResult))
	if err != nil || out.Summary != "done" || len(out.ChangedFiles) != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestGitDirResolutionFailureFailsClosed(t *testing.T) {
	_, err := Adapter{Workspaces: failingGitDir{}, Binary: fake(t, emit(recResult))}.Execute(context.Background(), agent.AgentExecutionInput{Workspace: repo(t), Task: "t"})
	requireKind(t, err, "claude_git")
}

// A linked worktree has a .git file; the guard must follow it to the shared Git
// directory, where the hooks and config live.
func TestLinkedWorktreeGuardsSharedGitDir(t *testing.T) {
	main := repo(t)
	tree := filepath.Join(t.TempDir(), "wt")
	gitTest(t, main, "worktree", "add", "-q", "-b", "side", tree)
	if info, err := os.Stat(filepath.Join(tree, ".git")); err != nil || info.IsDir() {
		t.Fatalf("expected a .git file: %v", err)
	}
	t.Setenv("COMMON_GIT_DIR", filepath.Join(main, ".git"))
	if _, err := executeFake(t, tree, "printf edited >file.txt\n"+emit(recResult), "COMMON_GIT_DIR"); err != nil {
		t.Fatalf("unchanged worktree run failed: %v", err)
	}
	_, err := executeFake(t, tree, "printf x >\"$COMMON_GIT_DIR/hooks/post-commit\"\n"+emit(recResult), "COMMON_GIT_DIR")
	requireKind(t, err, "claude_git")
}
