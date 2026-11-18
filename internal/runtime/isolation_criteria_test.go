package runtime_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

// F3.1 acceptance criteria that were not ticked when the feature closed: what
// reaches the unit commit after several attempts, and what each retain policy
// leaves on disk.

func TestIsolationUnitCommitOmitsFilesALaterAttemptRemoved(t *testing.T) {
	src := isoRepo(t)
	d := unitGraph(src, []string{"a"}, "a")
	d.Nodes["a"].Loop.MaxAttempts = 3
	a, count := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, attempt int) bool {
		exists := func(name string) bool {
			_, err := os.Stat(in.Workspace + "/" + name)
			return err == nil
		}
		switch attempt {
		case 1:
			isoWrite(t, in.Workspace, "scratch1.txt", "from attempt 1\n")
			isoWrite(t, in.Workspace, "a.txt", "A1\n")
		case 2:
			if !exists("scratch1.txt") {
				t.Errorf("attempt 2 must see the files attempt 1 left in the worktree")
			}
			isoWrite(t, in.Workspace, "scratch2.txt", "from attempt 2\n")
			isoWrite(t, in.Workspace, "a.txt", "A2\n")
		case 3:
			if !exists("scratch1.txt") || !exists("scratch2.txt") {
				t.Errorf("attempt 3 must see the files of attempts 1 and 2")
			}
			for _, name := range []string{"scratch1.txt", "scratch2.txt"} {
				if err := os.Remove(in.Workspace + "/" + name); err != nil {
					t.Errorf("removing %s: %v", name, err)
				}
			}
			isoWrite(t, in.Workspace, "a.txt", "A3\n")
		}
		return attempt == 3
	})
	result, err := runtime.Run(context.Background(), d, nil, compose(a), gitOptions(newGitWorkspaces(t), ""))
	if err != nil || result.State != run.RunSucceeded || result.Isolation == nil || len(result.Isolation.Units) != 1 {
		t.Fatalf("err=%v state=%s failure=%+v isolation=%+v", err, result.State, result.Failure, result.Isolation)
	}
	if got := count("a"); got != 3 {
		t.Fatalf("the unit ran %d attempts, want 3", got)
	}
	commit := result.Isolation.Units[0].Commit
	tree := strings.Fields(isoGit(t, src, "ls-tree", "-r", "--name-only", commit))
	for _, name := range tree {
		if strings.HasPrefix(name, "scratch") {
			t.Fatalf("the unit commit keeps %s, which attempt 3 removed; tree: %v", name, tree)
		}
	}
	if got := isoGit(t, src, "show", commit+":a.txt"); got != "A3" {
		t.Fatalf("unit commit holds a.txt = %q, want the accepted attempt's A3", got)
	}
}

// mixedRetainRun runs a accepted, b exhausted and c accepted under policy, each
// in its own worktree under a fresh root, and returns the result and the root.
func mixedRetainRun(t *testing.T, policy runtime.RetainPolicy) (run.Result, string) {
	t.Helper()
	src := isoRepo(t)
	d := unitGraph(src, []string{"a", "b", "c"}, "a", "b", "c")
	d.Nodes["b"].Loop.MaxAttempts = 2
	a, _ := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
		isoWrite(t, in.Workspace, in.Task+".txt", "edited by "+in.Task+"\n")
		return in.Task != "b"
	})
	workspaces := newGitWorkspaces(t)
	result, err := runtime.Run(context.Background(), d, nil, compose(a), gitOptions(workspaces, policy))
	if err != nil {
		t.Fatal(err)
	}
	if result.Failure == nil || result.Failure.Kind != run.FailureNeedsIntervention {
		t.Fatalf("the exhausted unit must end the run needing intervention: state=%s failure=%+v", result.State, result.Failure)
	}
	return result, workspaces.root
}

func TestIsolationRetainPoliciesLeaveTheDocumentedDirectoriesOnDisk(t *testing.T) {
	cases := []struct {
		policy runtime.RetainPolicy
		want   []string // nodes whose worktree directory stays on disk
	}{
		{runtime.RetainNever, nil},
		{runtime.RetainFailed, []string{"b"}},
		{runtime.RetainAlways, []string{"a", "b", "c"}},
	}
	for _, c := range cases {
		t.Run(string(c.policy), func(t *testing.T) {
			result, root := mixedRetainRun(t, c.policy)
			paths := map[string]string{}
			for _, e := range eventsOf(result, events.WorkspacePrepared) {
				paths[e.NodeID] = e.Payload.(events.WorkspacePreparedPayload).Path
			}
			if len(paths) != 3 {
				t.Fatalf("expected three prepared worktrees, got %v", paths)
			}
			var onDisk []string
			for node, path := range paths {
				_, err := os.Stat(path)
				if err == nil {
					onDisk = append(onDisk, node)
				} else if !errors.Is(err, fs.ErrNotExist) {
					t.Fatal(err)
				}
			}
			sort.Strings(onDisk)
			if strings.Join(onDisk, ",") != strings.Join(c.want, ",") {
				t.Fatalf("worktrees left on disk %v, want %v", onDisk, c.want)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(c.want) {
				t.Fatalf("the worktree root holds %d entries, want %d", len(entries), len(c.want))
			}
			if got := len(result.Isolation.Retained); got != len(c.want) {
				t.Fatalf("the result reports %d retained worktrees, want %d", got, len(c.want))
			}
		})
	}
}
