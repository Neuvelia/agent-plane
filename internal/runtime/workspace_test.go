package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/app/trace"
	"agentplane/internal/domain/run"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/testenv"
)

func workspaceGit(t *testing.T, root string, args ...string) {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = root
	c.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatal(e, string(b))
	}
}
func workspaceRepo(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	workspaceGit(t, root, "init", "-q")
	workspaceGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base")
	return root
}
func TestPhysicalWorkspaceIdentityConflicts(t *testing.T) {
	root := workspaceRepo(t)
	sub := filepath.Join(root, "sub")
	sibling := filepath.Join(root, "sibling")
	for _, p := range []string{sub, sibling} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(root, alias); e != nil {
		t.Fatal(e)
	}
	worktree := filepath.Join(t.TempDir(), "tree")
	workspaceGit(t, root, "worktree", "add", "--detach", "-q", worktree, "HEAD")
	nested := filepath.Join(root, "nested")
	if e := os.Mkdir(nested, 0700); e != nil {
		t.Fatal(e)
	}
	workspaceGit(t, nested, "init", "-q")
	for _, tc := range []struct {
		name, a, b string
		conflict   bool
	}{
		{"equal", root, root, true}, {"nested", root, nested, true}, {"subdir", root, sub, true}, {"sibling checkout dirs", sub, sibling, true}, {"symlink alias", root, alias, true}, {"missing", root, filepath.Join(root, "missing"), true}, {"non git", root, t.TempDir(), true}, {"distinct linked worktrees", root, worktree, false}, {"distinct checkouts", root, workspaceRepo(t), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := inspectWorkspace(context.Background(), tc.a)
			b := inspectWorkspace(context.Background(), tc.b)
			if got := workspaceConflict(a, b); got != tc.conflict {
				t.Fatalf("conflict=%t want=%t: %+v %+v", got, tc.conflict, a, b)
			}
			if !tc.conflict && (!a.known || !b.known) {
				t.Fatal("distinctness not proven")
			}
		})
	}
	// Malicious ambient Git environment must not redirect read-only inspection.
	t.Setenv("GIT_DIR", filepath.Join(root, "nonexistent"))
	if !inspectWorkspace(context.Background(), root).known {
		t.Fatal("inherited Git environment")
	}
}
func TestUnitModeSchedulingIncludesUnenrolledAgentsAndKeepsShells(t *testing.T) {
	root := workspaceRepo(t)
	other := workspaceRepo(t)
	for _, scope := range [][]string{{"one/"}, {"two/"}} {
		d := &graph.Definition{Units: []string{"a"}, Nodes: map[string]graph.NodeDefinition{
			"a": {Type: "agent", Config: graph.NodeConfig{"workspace": root, "allowed_paths": []string{"one/"}}},
			"b": {Type: "agent", Config: graph.NodeConfig{"workspace": root, "allowed_paths": scope}},
			"c": {Type: "shell"}, "d": {Type: "agent", Config: graph.NodeConfig{"workspace": other}},
		}}
		m := &runMachine{ids: []string{"a", "b", "c", "d"}, definition: d, nodes: map[string]*run.NodeResult{}}
		for _, id := range m.ids {
			m.nodes[id] = &run.NodeResult{State: run.NodeReady}
		}
		m.workspaces = inspectWorkspaces(context.Background(), d)
		batch := m.nextBatch(4)
		if len(batch) != 3 || batch[0] != "a" || batch[1] != "c" || batch[2] != "d" {
			t.Fatal("scope disjointness authorized unsafe concurrency", batch)
		}
		m.nodes["a"].State = run.NodeSucceeded
		batch = m.nextBatch(4)
		if batch[0] != "b" {
			t.Fatal("unstable order", batch)
		}
	}
}
func TestDeclaredPotentialConflictsAreBoundedRetainedAndDetached(t *testing.T) {
	root := workspaceRepo(t)
	d := &graph.Definition{Nodes: map[string]graph.NodeDefinition{}}
	for i := 0; i < 15; i++ {
		id := string(rune('a' + i))
		d.Nodes[id] = graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"workspace": root, "allowed_paths": []string{"src/"}}}
	}
	m := &runMachine{ids: sortedDefinitionIDs(d), emitter: trace.NewCollector(system.Clock{}, "r", "g", "v"), workspaces: inspectWorkspaces(context.Background(), d)}
	m.reportScopeConflicts(d)
	facts := m.emitter.Snapshot()
	if len(facts) != 1 {
		t.Fatal(facts)
	}
	p := facts[0].Payload.(events.DeclaredScopeConflictsPayload)
	if len(p.Details) != 64 || p.Omitted != 41 || p.Details[0].Reason != "declared_scope_overlap" {
		t.Fatal(p)
	}
	p.Details[0].Reason = "mutated"
	if m.emitter.Snapshot()[0].Payload.(events.DeclaredScopeConflictsPayload).Details[0].Reason != "declared_scope_overlap" {
		t.Fatal("diagnostic alias")
	}
	d.Nodes = map[string]graph.NodeDefinition{"a": {Type: "agent", Config: graph.NodeConfig{"workspace": "/missing", "allowed_paths": []string{"./"}}}, "b": {Type: "agent", Config: graph.NodeConfig{"workspace": root}}}
	m.ids = sortedDefinitionIDs(d)
	m.workspaces = inspectWorkspaces(context.Background(), d)
	m.emitter = trace.NewCollector(system.Clock{}, "r", "g", "v")
	m.reportScopeConflicts(d)
	p = m.emitter.Snapshot()[0].Payload.(events.DeclaredScopeConflictsPayload)
	if len(p.Details) != 2 || p.Details[0].Reason != "unknown_workspace_identity" || p.Details[1].Reason != "unknown_scope" {
		t.Fatal(p)
	}
}
