package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

func eventsOf(result run.Result, kind events.Type) []events.Event {
	var out []events.Event
	for _, e := range result.Events {
		if e.Type == kind {
			out = append(out, e)
		}
	}
	return out
}

func indexOfEvent(result run.Result, kind events.Type, node string) int {
	for i, e := range result.Events {
		if e.Type == kind && e.NodeID == node {
			return i
		}
	}
	return -1
}

func TestIsolationWorktreePersistsAcrossAttempts(t *testing.T) {
	source := t.TempDir()
	d := isolatedLoop(source)
	d.Nodes["work"].Loop.MaxAttempts = 3
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: []byte(fmt.Sprintf(`{"ok":%t}`, n == 3))}, nil
	}
	w := &fakeWorkspaces{root: t.TempDir()}
	result, err := runtime.Run(context.Background(), d, nil, compose(a), isolationOptions(w))
	if err != nil || len(a.calls) != 3 || len(w.created) != 1 {
		t.Fatalf("err=%v attempts=%d created=%v state=%s failure=%+v", err, len(a.calls), w.created, result.State, result.Failure)
	}
	for i, call := range a.calls {
		if call.Workspace != w.created[0] {
			t.Fatalf("attempt %d ran in %q, want the single worktree %q", i+1, call.Workspace, w.created[0])
		}
	}
	prepared := eventsOf(result, events.WorkspacePrepared)
	if len(prepared) != 1 {
		t.Fatalf("%d workspace_prepared events", len(prepared))
	}
	payload := prepared[0].Payload.(events.WorkspacePreparedPayload)
	if payload.BaseSHA != fakeBaseSHA || payload.Mode != events.WorkspaceModeWorktree || payload.Path != w.created[0] || payload.SourceRoot != source {
		t.Fatalf("payload %+v", payload)
	}
	if prepared[0].NodeID != "work" || prepared[0].NodeRunID == "" || prepared[0].AttemptID != "" {
		t.Fatalf("correlation %+v", prepared[0])
	}
	if indexOfEvent(result, events.WorkspacePrepared, "work") > indexOfEvent(result, events.AttemptStarted, "work") {
		t.Fatal("workspace_prepared must precede the first attempt")
	}
	if got := d.Nodes["work"].Config["workspace"]; got != source {
		t.Fatalf("the graph definition was mutated: workspace %v", got)
	}
}

func TestIsolationCreateFailureIsAnOperationalErrorAndSiblingsContinue(t *testing.T) {
	source := t.TempDir()
	d := unitGraph(source, []string{"a", "b"}, "a")
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: []byte(`{"ok":true}`)}, nil
	}
	w := &fakeWorkspaces{root: t.TempDir(), createErr: errors.New("disk full")}
	options := isolationOptions(w)
	options.MaxConcurrency = 2
	result, err := runtime.Run(context.Background(), d, nil, compose(a), options)
	if err != nil || result.State != run.RunFailed || result.Failure == nil || result.Failure.NodeID != "a" || result.Failure.Kind != run.FailureExecutor {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	states := map[string]run.NodeState{}
	for _, n := range result.Nodes {
		states[n.NodeID] = n.State
	}
	if states["a"] != run.NodeFailed || states["b"] != run.NodeSucceeded {
		t.Fatalf("states %v", states)
	}
	for _, e := range result.Events {
		if e.NodeID != "a" {
			continue
		}
		switch e.Type {
		case events.AttemptStarted, events.GateCheck, events.ReturnEnvelope, events.WorkspacePrepared:
			t.Fatalf("node a emitted %s after a failed worktree creation", e.Type)
		}
	}
	for _, e := range eventsOf(result, events.NodeFinished) {
		if payload := e.Payload.(events.NodeFinishedPayload); e.NodeID == "a" && strings.Contains(strings.Join(payload.DiagnosticCodes, ","), "disk") {
			t.Fatalf("raw error text reached an event: %+v", payload)
		}
	}
}

func okAdapter() *loopAdapter {
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: []byte(`{"ok":true}`)}, nil
	}
	return a
}

func TestIsolationEventOrder(t *testing.T) {
	w := &fakeWorkspaces{root: t.TempDir()}
	result, err := runtime.Run(context.Background(), isolatedLoop(t.TempDir()), nil, compose(okAdapter()), isolationOptions(w))
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	var order []events.Type
	for _, e := range result.Events {
		if e.NodeID == "work" {
			order = append(order, e.Type)
		}
	}
	want := []events.Type{events.NodeReady, events.NodeStarted, events.WorkspacePrepared, events.AttemptStarted, events.AttemptInput,
		events.GateCheck, events.ReturnEnvelope, events.AttemptFinished, events.WorkspaceIntegrated, events.NodeFinished}
	var got []events.Type
	for _, kind := range order {
		for _, w := range want {
			if kind == w {
				got = append(got, kind)
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order: got %v, want %v", got, want)
	}
	integrated := eventsOf(result, events.WorkspaceIntegrated)[0]
	finished := eventsOf(result, events.AttemptFinished)[0]
	payload := integrated.Payload.(events.WorkspaceIntegratedPayload)
	if integrated.AttemptID != finished.AttemptID || integrated.AttemptID != nodeResult(result, "work").AcceptedAttemptID {
		t.Fatalf("workspace_integrated must carry the accepted attempt: %+v", integrated)
	}
	if payload.UnitRef != "refs/agentplane/fake/units/work" || payload.IntegratedRef != "refs/agentplane/fake/integrated" ||
		payload.UnitCommit != fakeCommit(1) || payload.IntegratedCommit != fakeCommit(101) || payload.ChangedPaths != 1 {
		t.Fatalf("payload %+v", payload)
	}
}

func TestIsolationIntegratesInSortedNodeOrderWhateverFinishesFirst(t *testing.T) {
	source := t.TempDir()
	d := unitGraph(source, []string{"a", "b"}, "a", "b")
	bDone := make(chan struct{})
	a := newLoopAdapter()
	a.produce = func(ctx context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		if in.Task == "a" {
			select {
			case <-bDone:
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
			}
		} else {
			close(bDone)
		}
		return agent.AgentExecutionOutput{RawMetadata: []byte(`{"ok":true}`)}, nil
	}
	w := &fakeWorkspaces{root: t.TempDir()}
	options := isolationOptions(w)
	options.MaxConcurrency = 2
	result, err := runtime.Run(context.Background(), d, nil, compose(a), options)
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if fmt.Sprint(w.snapshots) != "[refs/agentplane/fake/units/a refs/agentplane/fake/units/b]" {
		t.Fatalf("snapshot order %v", w.snapshots)
	}
	if fmt.Sprint(w.integrated) != fmt.Sprint([]string{fakeCommit(1), fakeCommit(2)}) {
		t.Fatalf("integration order %v", w.integrated)
	}
}

func TestIsolationExhaustedUnitProducesNoRef(t *testing.T) {
	d := isolatedLoop(t.TempDir())
	d.Nodes["work"].Loop.MaxAttempts = 2
	w := &fakeWorkspaces{root: t.TempDir()}
	result, err := runtime.Run(context.Background(), d, nil, compose(newLoopAdapter()), isolationOptions(w))
	if err != nil || result.Failure == nil || result.Failure.Kind != run.FailureNeedsIntervention {
		t.Fatalf("err=%v failure=%+v", err, result.Failure)
	}
	if len(w.snapshots)+len(w.integrated) != 0 || countEvents(result, events.WorkspaceIntegrated) != 0 {
		t.Fatalf("a rejected unit was snapshotted or integrated: %v %v", w.snapshots, w.integrated)
	}
}

func TestIsolationOperationalFailureDoesNotRewriteAcceptedValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*fakeWorkspaces)
	}{
		{"snapshot", func(w *fakeWorkspaces) { w.snapshotErr = errors.New("git: object database is corrupt") }},
		{"integrate", func(w *fakeWorkspaces) { w.integrateErr = errors.New("git: cannot advance ref") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := unitGraph(t.TempDir(), []string{"a", "b"}, "a")
			a := okAdapter()
			w := &fakeWorkspaces{root: t.TempDir()}
			tc.set(w)
			options := isolationOptions(w)
			options.MaxConcurrency = 2
			result, err := runtime.Run(context.Background(), d, nil, compose(a), options)
			if err != nil || result.State != run.RunFailed || result.Failure.Kind != run.FailureExecutor || result.Failure.NodeID != "a" {
				t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
			}
			if len(a.calls) != 2 {
				t.Fatalf("an accepted unit was rerun: %d invocations", len(a.calls))
			}
			node := result.Nodes[0]
			if node.State != run.NodeFailed || len(node.Outputs) == 0 || node.AcceptedAttemptID == "" {
				t.Fatalf("accepted values must stay recorded on the failed node: %+v", node)
			}
			if result.Nodes[1].State != run.NodeSucceeded {
				t.Fatalf("the shared-checkout sibling must be unaffected: %+v", result.Nodes[1])
			}
			if countEvents(result, events.WorkspaceIntegrated) != 0 {
				t.Fatal("an integration was reported")
			}
			finished := eventsOf(result, events.NodeFinished)
			for _, e := range finished {
				for _, code := range e.Payload.(events.NodeFinishedPayload).DiagnosticCodes {
					if strings.Contains(code, "git:") {
						t.Fatalf("raw error text in an event: %q", code)
					}
				}
			}
		})
	}
}

func nodeResult(result run.Result, id string) run.NodeResult {
	for _, n := range result.Nodes {
		if n.NodeID == id {
			return n
		}
	}
	return run.NodeResult{}
}

func TestIsolationConflictNeedsInterventionKeepsAcceptedValues(t *testing.T) {
	d := unitGraph(t.TempDir(), []string{"a", "b"}, "a", "b")
	d.Nodes["tail"] = graph.NodeDefinition{Type: "shell", Inputs: map[string]graph.PortDefinition{"result": {Type: graph.ValueTypeJSON, Required: true}}}
	d.Edges = []graph.EdgeDefinition{{From: "b.outputs.raw_metadata", To: "tail.inputs.result"}}
	a := okAdapter()
	w := &fakeWorkspaces{root: t.TempDir(), conflict: map[string][]string{"b": {"shared.txt", "docs/x.md"}}}
	options := isolationOptions(w)
	options.MaxConcurrency = 2
	result, err := runtime.Run(context.Background(), d, nil, compose(a), options)
	if err != nil || result.State != run.RunFailed || result.Failure == nil || result.Failure.Kind != run.FailureNeedsIntervention || result.Failure.NodeID != "b" {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	if len(a.calls) != 2 {
		t.Fatalf("a conflict reran a unit: %d invocations", len(a.calls))
	}
	nodeA, nodeB, tail := nodeResult(result, "a"), nodeResult(result, "b"), nodeResult(result, "tail")
	if nodeA.State != run.NodeSucceeded || len(nodeA.Outputs) == 0 {
		t.Fatalf("the accepted sibling changed: %+v", nodeA)
	}
	if nodeB.State != run.NodeFailed || len(nodeB.Outputs) == 0 || nodeB.AcceptedAttemptID == "" {
		t.Fatalf("recorded outputs and accepted attempt must stay on the conflicting node: %+v", nodeB)
	}
	if tail.State != run.NodeSkipped {
		t.Fatalf("a dependent of the conflicting unit ran: %+v", tail)
	}
	conflicts := eventsOf(result, events.WorkspaceConflict)
	if len(conflicts) != 1 || conflicts[0].NodeID != "b" || conflicts[0].AttemptID != nodeB.AcceptedAttemptID {
		t.Fatalf("conflict events %+v", conflicts)
	}
	payload := conflicts[0].Payload.(events.WorkspaceConflictPayload)
	integratedA := eventsOf(result, events.WorkspaceIntegrated)
	if len(integratedA) != 1 || integratedA[0].NodeID != "a" {
		t.Fatalf("only the clean unit is integrated: %+v", integratedA)
	}
	if payload.TipCommit != integratedA[0].Payload.(events.WorkspaceIntegratedPayload).IntegratedCommit || fmt.Sprint(payload.Paths) != "[docs/x.md shared.txt]" || payload.Omitted != 0 || payload.UnitCommit != fakeCommit(2) {
		t.Fatalf("payload %+v", payload)
	}
	for _, e := range eventsOf(result, events.NodeFinished) {
		finished := e.Payload.(events.NodeFinishedPayload)
		if e.NodeID == "b" && (finished.FailureKind != "needs_intervention" || strings.Join(finished.DiagnosticCodes, ",") != "E_WORKSPACE_CONFLICT") {
			t.Fatalf("node_finished %+v", finished)
		}
	}
}

func TestIsolationConflictWithoutPathsIsAnOperationalError(t *testing.T) {
	w := &fakeWorkspaces{root: t.TempDir(), conflict: map[string][]string{"work": {}}}
	result, err := runtime.Run(context.Background(), isolatedLoop(t.TempDir()), nil, compose(okAdapter()), isolationOptions(w))
	if err != nil || result.Failure == nil || result.Failure.Kind != run.FailureExecutor || countEvents(result, events.WorkspaceConflict) != 0 {
		t.Fatalf("err=%v failure=%+v", err, result.Failure)
	}
}

func TestIsolationMalformedManagerResultFailsTheNodeInsteadOfPanicking(t *testing.T) {
	w := &fakeWorkspaces{root: t.TempDir(), malformedTip: true}
	result, err := runtime.Run(context.Background(), isolatedLoop(t.TempDir()), nil, compose(okAdapter()), isolationOptions(w))
	if err != nil || result.Failure == nil || result.Failure.Kind != run.FailureExecutor || countEvents(result, events.WorkspaceIntegrated) != 0 {
		t.Fatalf("err=%v failure=%+v", err, result.Failure)
	}
}

type releaseFact struct{ action, reason string }

func releases(result run.Result) map[string]releaseFact {
	out := map[string]releaseFact{}
	for _, e := range eventsOf(result, events.WorkspaceReleased) {
		p := e.Payload.(events.WorkspaceReleasedPayload)
		out[e.NodeID] = releaseFact{p.Action, p.Reason}
	}
	return out
}

func TestIsolationRetentionTable(t *testing.T) {
	ids := []string{"conflict", "err", "ok", "red"}
	for _, tc := range []struct {
		policy runtime.RetainPolicy
		want   map[string]releaseFact
	}{
		{"", map[string]releaseFact{"ok": {"removed", "integrated"}, "conflict": {"retained", "conflict"}, "err": {"retained", "error"}, "red": {"retained", "exhausted"}}},
		{runtime.RetainFailed, map[string]releaseFact{"ok": {"removed", "integrated"}, "conflict": {"retained", "conflict"}, "err": {"retained", "error"}, "red": {"retained", "exhausted"}}},
		{runtime.RetainNever, map[string]releaseFact{"ok": {"removed", "integrated"}, "conflict": {"removed", "policy_never"}, "err": {"removed", "policy_never"}, "red": {"removed", "policy_never"}}},
		{runtime.RetainAlways, map[string]releaseFact{"ok": {"retained", "policy_always"}, "conflict": {"retained", "policy_always"}, "err": {"retained", "policy_always"}, "red": {"retained", "policy_always"}}},
	} {
		t.Run(string(tc.policy), func(t *testing.T) {
			a := newLoopAdapter()
			a.produce = func(_ context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
				if in.Task == "err" {
					return agent.AgentExecutionOutput{}, errors.New("adapter failed")
				}
				return agent.AgentExecutionOutput{RawMetadata: []byte(fmt.Sprintf(`{"ok":%t}`, in.Task != "red"))}, nil
			}
			w := &fakeWorkspaces{root: t.TempDir(), conflict: map[string][]string{"conflict": {"shared.txt"}}}
			options := isolationOptions(w)
			options.MaxConcurrency = 4
			options.RetainWorktrees = tc.policy
			result, err := runtime.Run(context.Background(), unitGraph(t.TempDir(), ids, ids...), nil, compose(a), options)
			if err != nil {
				t.Fatal(err)
			}
			got := releases(result)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("releases: got %v, want %v", got, tc.want)
			}
			var removed []string
			for node, fact := range tc.want {
				if fact.action == "removed" {
					removed = append(removed, w.created[indexOfCreated(t, w, result, node)])
				}
			}
			sort.Strings(removed)
			sort.Strings(w.removed)
			if fmt.Sprint(removed) != fmt.Sprint(w.removed) {
				t.Fatalf("directories removed %v, want %v", w.removed, removed)
			}
			for _, e := range eventsOf(result, events.WorkspaceReleased) {
				if p := e.Payload.(events.WorkspaceReleasedPayload); !strings.HasPrefix(p.Path, w.root) {
					t.Fatalf("released path %q is not a created worktree", p.Path)
				}
			}
		})
	}
}

// indexOfCreated finds the position of node's worktree in w.created through its
// workspace_prepared event.
func indexOfCreated(t *testing.T, w *fakeWorkspaces, result run.Result, node string) int {
	t.Helper()
	for _, e := range eventsOf(result, events.WorkspacePrepared) {
		if e.NodeID == node {
			path := e.Payload.(events.WorkspacePreparedPayload).Path
			for i, created := range w.created {
				if created == path {
					return i
				}
			}
		}
	}
	t.Fatalf("no worktree for %s", node)
	return -1
}

func TestIsolationCleanupFailureDoesNotChangeStatus(t *testing.T) {
	w := &fakeWorkspaces{root: t.TempDir(), removeErr: errors.New("rm: device busy /secret/path")}
	result, err := runtime.Run(context.Background(), isolatedLoop(t.TempDir()), nil, compose(okAdapter()), isolationOptions(w))
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	released := eventsOf(result, events.WorkspaceReleased)
	if len(released) != 1 {
		t.Fatalf("%d released events", len(released))
	}
	p := released[0].Payload.(events.WorkspaceReleasedPayload)
	if p.Action != events.WorkspaceRetained || p.Reason != events.ReleaseCleanupError || p.CleanupError == "" || strings.Contains(p.CleanupError, "busy") || p.Path != w.created[0] {
		t.Fatalf("payload %+v", p)
	}
}

func TestIsolationCancellationReleasesAfterProcessEnds(t *testing.T) {
	d := unitGraph(t.TempDir(), []string{"a", "b"}, "a", "b")
	ctx, cancel := context.WithCancel(context.Background())
	var entered, returned atomic.Int32
	both := make(chan struct{})
	a := newLoopAdapter()
	a.produce = func(ctx context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		if entered.Add(1) == 2 {
			close(both)
		}
		<-ctx.Done()
		returned.Add(1)
		return agent.AgentExecutionOutput{}, ctx.Err()
	}
	var early atomic.Bool
	w := &fakeWorkspaces{root: t.TempDir(), onRemove: func() {
		if returned.Load() != 2 {
			early.Store(true)
		}
	}}
	go func() {
		<-both
		cancel()
	}()
	options := isolationOptions(w)
	options.MaxConcurrency = 2
	options.RetainWorktrees = runtime.RetainNever
	result, err := runtime.Run(ctx, d, nil, compose(a), options)
	if err != nil || result.State != run.RunCancelled {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if early.Load() || len(w.removed) != 2 {
		t.Fatalf("removal began before the agent processes ended (early=%t) or was skipped: %v", early.Load(), w.removed)
	}
	got := releases(result)
	if len(got) != 2 || got["a"].reason != "policy_never" || got["b"].action != "removed" {
		t.Fatalf("releases %v", got)
	}
	retained, err := runtime.Run(ctx, d, nil, compose(newLoopAdapter()), isolationOptions(&fakeWorkspaces{root: t.TempDir()}))
	if err != nil || retained.State != run.RunCancelled {
		t.Fatalf("pre-cancelled run: err=%v state=%s", err, retained.State)
	}
}

func TestIsolationResultReportsRefsCommitsAndRetainedPaths(t *testing.T) {
	ids := []string{"conflict", "err", "ok", "red"}
	a := newLoopAdapter()
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		if in.Task == "err" {
			return agent.AgentExecutionOutput{}, errors.New("adapter failed")
		}
		return agent.AgentExecutionOutput{RawMetadata: []byte(fmt.Sprintf(`{"ok":%t}`, in.Task != "red"))}, nil
	}
	w := &fakeWorkspaces{root: t.TempDir(), conflict: map[string][]string{"conflict": {"shared.txt"}}}
	options := isolationOptions(w)
	options.MaxConcurrency = 4
	result, err := runtime.Run(context.Background(), unitGraph(t.TempDir(), ids, ids...), nil, compose(a), options)
	if err != nil || result.Isolation == nil {
		t.Fatalf("err=%v isolation=%v", err, result.Isolation)
	}
	got := *result.Isolation
	tip := eventsOf(result, events.WorkspaceIntegrated)[0].Payload.(events.WorkspaceIntegratedPayload).IntegratedCommit
	want := run.IsolationResult{
		BaseSHA: fakeBaseSHA, IntegratedRef: "refs/agentplane/fake/integrated", IntegratedCommit: tip,
		Units: []run.IsolatedUnit{
			{NodeID: "conflict", Ref: "refs/agentplane/fake/units/conflict", Commit: fakeCommit(1)},
			{NodeID: "ok", Ref: "refs/agentplane/fake/units/ok", Commit: fakeCommit(2), Integrated: true},
		},
		Retained: []run.RetainedWorkspace{
			{NodeID: "conflict", Path: w.created[indexOfCreated(t, w, result, "conflict")], Reason: "conflict"},
			{NodeID: "err", Path: w.created[indexOfCreated(t, w, result, "err")], Reason: "error"},
			{NodeID: "red", Path: w.created[indexOfCreated(t, w, result, "red")], Reason: "exhausted"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("isolation result: got %+v, want %+v", got, want)
	}
}

func TestIsolationResultAbsentWithoutIsolation(t *testing.T) {
	d := loopDefinition(1)
	d.Units = []string{"work"}
	result, err := runtime.Run(context.Background(), d, nil, compose(okAdapter()), isolationOptions(nil))
	if err != nil || result.Isolation != nil {
		t.Fatalf("err=%v isolation=%+v", err, result.Isolation)
	}
}

func TestIsolationCancelledDuringCreateLeavesNoRunningNode(t *testing.T) {
	d := unitGraph(t.TempDir(), []string{"a", "b"}, "a", "b")
	ctx, cancel := context.WithCancel(context.Background())
	w := &fakeWorkspaces{root: t.TempDir(), onCreate: cancel}
	options := isolationOptions(w)
	options.MaxConcurrency = 2
	result, err := runtime.Run(ctx, d, nil, compose(okAdapter()), options)
	if err != nil || result.State != run.RunCancelled {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	for _, n := range result.Nodes {
		if n.State != run.NodeCancelled {
			t.Fatalf("node %s ended %s", n.NodeID, n.State)
		}
	}
	started, terminated := countEvents(result, events.NodeStarted), countEvents(result, events.NodeFinished)+countEvents(result, events.NodeTerminated)
	if terminated != 2 || started > terminated {
		t.Fatalf("started=%d terminal=%d", started, terminated)
	}
}
