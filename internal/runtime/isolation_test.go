package runtime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

func isolationOptions(w runtime.WorkspaceManager) runtime.Options {
	return runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Workspaces: w}
}

// isolatedLoop is one enrolled loop Agent whose workspace is source.
func isolatedLoop(source string) *graph.Definition {
	d := loopDefinition(1)
	n := d.Nodes["work"]
	n.Config["workspace"] = source
	n.Config["isolation"] = "worktree"
	d.Nodes["work"] = n
	d.Units = []string{"work"}
	return d
}

func requirePreflightFailure(t *testing.T, d *graph.Definition, options runtime.Options, mention string) {
	t.Helper()
	a := newLoopAdapter()
	var collected []events.Event
	live := make(chan events.Event, 1024)
	options.LiveEvents = live
	result, err := runtime.Run(context.Background(), d, nil, compose(a), options)
	close(live)
	for e := range live {
		collected = append(collected, e)
	}
	var preflight *run.PreflightError
	if !errors.As(err, &preflight) {
		t.Fatalf("want a PreflightError, got %v (state %s)", err, result.State)
	}
	if !strings.Contains(err.Error(), mention) {
		t.Fatalf("error %q does not mention %q", err, mention)
	}
	if len(collected) != 0 || len(result.Events) != 0 || len(a.calls) != 0 {
		t.Fatalf("preflight failure emitted %d events and ran %d attempts", len(collected), len(a.calls))
	}
}

func TestIsolationPreflightFailsBeforeRunStarted(t *testing.T) {
	source := t.TempDir()
	t.Run("not enrolled in units", func(t *testing.T) {
		d := isolatedLoop(source)
		d.Units = nil
		requirePreflightFailure(t, d, isolationOptions(&fakeWorkspaces{}), "work")
	})
	t.Run("not a loop", func(t *testing.T) {
		d := isolatedLoop(source)
		n := d.Nodes["work"]
		n.Loop = nil
		d.Nodes["work"] = n
		requirePreflightFailure(t, d, isolationOptions(&fakeWorkspaces{}), "loop")
	})
	t.Run("not an agent", func(t *testing.T) {
		d := isolatedLoop(source)
		n := d.Nodes["down"]
		n.Config = graph.NodeConfig{"isolation": "worktree"}
		d.Nodes["down"] = n
		requirePreflightFailure(t, d, isolationOptions(&fakeWorkspaces{}), "down")
	})
	t.Run("invalid value", func(t *testing.T) {
		for _, value := range []any{"clone", "", true, 3} {
			d := isolatedLoop(source)
			d.Nodes["work"].Config["isolation"] = value
			requirePreflightFailure(t, d, isolationOptions(&fakeWorkspaces{}), "isolation")
		}
	})
	t.Run("no workspace manager", func(t *testing.T) {
		requirePreflightFailure(t, isolatedLoop(source), isolationOptions(nil), "workspace")
	})
	t.Run("source rejected by the manager", func(t *testing.T) {
		w := &fakeWorkspaces{preflightErr: errors.New("source workspace has uncommitted tracked changes")}
		requirePreflightFailure(t, isolatedLoop(source), isolationOptions(w), "uncommitted tracked changes")
	})
	t.Run("invalid retention policy", func(t *testing.T) {
		o := isolationOptions(&fakeWorkspaces{})
		o.RetainWorktrees = "sometimes"
		requirePreflightFailure(t, isolatedLoop(source), o, "retain")
	})
	t.Run("two different source repositories", func(t *testing.T) {
		d := isolatedLoop(source)
		second := d.Nodes["work"]
		second.Config = graph.NodeConfig{"adapter": "fake", "task": "t", "workspace": t.TempDir(), "isolation": "worktree", "allowed_paths": []string{"x"}}
		d.Nodes["second"] = second
		d.Units = []string{"second", "work"}
		requirePreflightFailure(t, d, isolationOptions(&fakeWorkspaces{}), "share one source")
	})
}

func TestIsolationPreflightCallsManagerOncePerDistinctSource(t *testing.T) {
	source := t.TempDir()
	d := isolatedLoop(source)
	other := d.Nodes["work"]
	other.Config = graph.NodeConfig{"adapter": "fake", "task": "t", "workspace": source, "isolation": "worktree", "allowed_paths": []string{"other"}}
	d.Nodes["other"] = other
	d.Units = []string{"other", "work"}
	w := &fakeWorkspaces{root: t.TempDir(), preflightErr: errors.New("stop here")}
	_, err := runtime.Run(context.Background(), d, nil, compose(newLoopAdapter()), isolationOptions(w))
	if err == nil || len(w.preflights) != 1 || w.preflights[0] != source {
		t.Fatalf("err=%v preflights=%v", err, w.preflights)
	}
}

func TestIsolationAbsentNeverTouchesTheManager(t *testing.T) {
	d := loopDefinition(1)
	d.Units = []string{"work"}
	for _, value := range []any{nil, "none"} {
		if value != nil {
			d.Nodes["work"].Config["isolation"] = value
		}
		w := &fakeWorkspaces{}
		result, err := runtime.Run(context.Background(), d, nil, compose(newLoopAdapter()), isolationOptions(w))
		if err != nil || len(w.preflights)+len(w.created) != 0 {
			t.Fatalf("value %v: err=%v preflights=%v", value, err, w.preflights)
		}
		for _, e := range result.Events {
			switch e.Type {
			case events.WorkspacePrepared, events.WorkspaceIntegrated, events.WorkspaceConflict, events.WorkspaceReleased, events.SourceWorkspaceChanged:
				t.Fatalf("workspace fact %s without isolation", e.Type)
			}
		}
	}
}

func TestIsolationAbsentPreservesTheSemanticHash(t *testing.T) {
	d := loopDefinition(1)
	d.Units = []string{"work"}
	before, err := graph.SemanticVersion(d, yamlgraph.Marshal)
	if err != nil {
		t.Fatal(err)
	}
	isolated := isolatedLoop(d.Nodes["work"].Config["workspace"].(string))
	after, err := graph.SemanticVersion(isolated, yamlgraph.Marshal)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("isolation is semantic content and must change the semantic version when present")
	}
	again, err := graph.SemanticVersion(d, yamlgraph.Marshal)
	if err != nil || again != before {
		t.Fatalf("hash of a graph without isolation is not stable: %v %q %q", err, before, again)
	}
}

// unitGraph builds independent enrolled loop Agents sharing one workspace
// string. isolated lists the node IDs that opt in to a worktree.
func unitGraph(source string, ids []string, isolated ...string) *graph.Definition {
	d := loopDefinition(1)
	delete(d.Nodes, "work")
	delete(d.Nodes, "down")
	d.Edges = []graph.EdgeDefinition{}
	for _, id := range ids {
		n := loopDefinition(1).Nodes["work"]
		n.Config["task"] = id
		n.Config["workspace"] = source
		n.Config["allowed_paths"] = []string{id + ".txt"}
		for _, want := range isolated {
			if want == id {
				n.Config["isolation"] = "worktree"
			}
		}
		d.Nodes[id] = n
		d.Units = append(d.Units, id)
	}
	return d
}

// barrierAdapter makes every first attempt wait until n attempts are running.
// It reports whether the barrier opened before the guard deadline.
func barrierAdapter(n int, opened *sync.Map) *loopAdapter {
	a := newLoopAdapter()
	var mu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	a.produce = func(ctx context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		mu.Lock()
		arrived++
		if arrived == n {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
			opened.Store(in.Task, true)
		case <-ctx.Done():
			return agent.AgentExecutionOutput{}, ctx.Err()
		case <-time.After(10 * time.Second):
		}
		return agent.AgentExecutionOutput{RawMetadata: []byte(`{"ok":true}`)}, nil
	}
	return a
}

func countEvents(result run.Result, kind events.Type) int {
	n := 0
	for _, e := range result.Events {
		if e.Type == kind {
			n++
		}
	}
	return n
}

func TestIsolationSchedulesIsolatedNodesConcurrently(t *testing.T) {
	source := t.TempDir()
	for _, tc := range []struct {
		name     string
		isolated []string
	}{
		{"both isolated", []string{"a", "b"}},
		{"isolated beside a shared-checkout agent", []string{"a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened sync.Map
			a := barrierAdapter(2, &opened)
			options := isolationOptions(&fakeWorkspaces{root: t.TempDir()})
			options.MaxConcurrency = 2
			result, err := runtime.Run(context.Background(), unitGraph(source, []string{"a", "b"}, tc.isolated...), nil, compose(a), options)
			if err != nil || result.State != run.RunSucceeded {
				t.Fatalf("state=%s err=%v failure=%+v", result.State, err, result.Failure)
			}
			_, a1 := opened.Load("a")
			_, b1 := opened.Load("b")
			if !a1 || !b1 {
				t.Fatal("the two attempts never ran at the same time")
			}
		})
	}
}

func TestIsolationReportsNoScopeConflictBetweenIsolatedNodes(t *testing.T) {
	source := t.TempDir()
	run2 := func(isolated ...string) run.Result {
		options := isolationOptions(&fakeWorkspaces{root: t.TempDir()})
		options.MaxConcurrency = 2
		result, err := runtime.Run(context.Background(), unitGraph(source, []string{"a", "b"}, isolated...), nil, compose(newLoopAdapter()), options)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if countEvents(run2(), events.DeclaredScopeConflicts) == 0 {
		t.Fatal("control: unknown shared checkouts must still be reported")
	}
	if n := countEvents(run2("a", "b"), events.DeclaredScopeConflicts); n != 0 {
		t.Fatalf("%d declared_scope_conflicts for isolated nodes", n)
	}
}
