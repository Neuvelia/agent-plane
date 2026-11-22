package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/adapter/attemptdiff"
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

// realDiffer composes internal/adapter/attemptdiff behind the runtime port, as
// the CLI does. Tests live in runtime_test so production runtime never imports it.
type realDiffer struct{ sessions *[]*attemptdiff.Recorder }

func (d realDiffer) Open(context.Context) (runtime.DiffSession, error) {
	r, err := attemptdiff.Open()
	if err != nil {
		return nil, err
	}
	if d.sessions != nil {
		*d.sessions = append(*d.sessions, r)
	}
	return realSession{r}, nil
}

type realSession struct{ r *attemptdiff.Recorder }

func (s realSession) Snapshot(ctx context.Context, workspace string) (runtime.DiffSnapshot, error) {
	return s.r.Snapshot(ctx, workspace)
}

func (s realSession) Diff(ctx context.Context, before, after runtime.DiffSnapshot, red runtime.DiffRedactor) (runtime.AttemptCapture, error) {
	got, err := s.r.Diff(ctx, before.(attemptdiff.Snapshot), after.(attemptdiff.Snapshot), red)
	return runtime.AttemptCapture{Files: got.Files, Summary: got.Summary}, err
}

func (s realSession) Close() error { return s.r.Close() }

// fakeDiffer is a hand-written port fake; every field is optional.
type fakeDiffer struct {
	mu        sync.Mutex
	opened    int
	closed    int
	snapshots int
	diffs     int
	openErr   error
	snapErr   func(call int) error
	diffErr   error
	capture   runtime.AttemptCapture
}

func (f *fakeDiffer) Open(context.Context) (runtime.DiffSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openErr != nil {
		return nil, f.openErr
	}
	f.opened++
	return fakeSession{f}, nil
}

type fakeSession struct{ f *fakeDiffer }

func (s fakeSession) Snapshot(context.Context, string) (runtime.DiffSnapshot, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.snapshots++
	if s.f.snapErr != nil {
		if err := s.f.snapErr(s.f.snapshots); err != nil {
			return nil, err
		}
	}
	return s.f.snapshots, nil
}

func (s fakeSession) Diff(context.Context, runtime.DiffSnapshot, runtime.DiffSnapshot, runtime.DiffRedactor) (runtime.AttemptCapture, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.diffs++
	return s.f.capture, s.f.diffErr
}

func (s fakeSession) Close() error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.closed++
	return nil
}

func diffOptions(d runtime.AttemptDiffer) runtime.Options {
	return runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, AttemptDiffs: d}
}

// redThenGreen is a producer that fails the Gate on attempt 1 and passes later;
// edit runs inside the workspace before each answer.
func redThenGreen(t *testing.T, edit func(workspace string, attempt int)) *loopAdapter {
	t.Helper()
	a := newLoopAdapter()
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		if edit != nil {
			edit(in.Workspace, n)
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(fmt.Sprintf(`{"ok":%t}`, n >= 2))}, nil
	}
	return a
}

func repoGraph(dir string, budget int) *graph.Definition {
	d := loopDefinition(budget)
	d.Nodes["work"].Config["workspace"] = dir
	return d
}

func diffEvents(r run.Result) []events.Event { return eventsOf(r, events.AttemptDiff) }

func TestNoAttemptDiffWithoutOption(t *testing.T) {
	dir := isoRepo(t)
	a := redThenGreen(t, func(ws string, _ int) { isoWrite(t, ws, "a.txt", "changed\n") })
	r, err := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(a), diffOptions(nil))
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("%v %+v", err, r)
	}
	if got := diffEvents(r); len(got) != 0 {
		t.Fatalf("attempt_diff without the option: %d events", len(got))
	}
}

func TestAttemptDiffEventOrderAndAttribution(t *testing.T) {
	dir := isoRepo(t)
	isoWrite(t, dir, "shared.txt", "one\ntwo\nthree\ndirty\n") // pre-existing dirt
	isoWrite(t, dir, "untracked.txt", "x\n")
	a := redThenGreen(t, func(ws string, attempt int) {
		isoWrite(t, ws, "a.txt", fmt.Sprintf("attempt %d\n", attempt))
		if attempt == 2 {
			isoWrite(t, ws, "new.txt", "created in two\n")
		}
	})
	var sessions []*attemptdiff.Recorder
	r, err := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(a), diffOptions(realDiffer{&sessions}))
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("%v %+v", err, r.Failure)
	}
	perAttempt := map[events.AttemptID][]events.AttemptDiffPayload{}
	var order []string
	attemptOf := []events.AttemptID{}
	for _, e := range r.Events {
		switch e.Type {
		case events.AttemptDiff:
			perAttempt[e.AttemptID] = append(perAttempt[e.AttemptID], e.Payload.(events.AttemptDiffPayload))
			order = append(order, "diff")
		case events.AttemptFinished:
			order = append(order, "finished")
			attemptOf = append(attemptOf, e.AttemptID)
		case events.GateCheck:
			order = append(order, "gate")
		}
	}
	if !regexp.MustCompile(`^gate(,diff)+,finished,gate(,diff)+,finished$`).MatchString(strings.Join(order, ",")) {
		t.Fatalf("each attempt's diffs sit between its gate_check and attempt_finished: %v", order)
	}
	if len(attemptOf) != 2 {
		t.Fatalf("attempts finished = %v", attemptOf)
	}
	first, second := perAttempt[attemptOf[0]], perAttempt[attemptOf[1]]
	if got := payloadPaths(first); !reflect.DeepEqual(got, []string{"a.txt", ""}) {
		t.Fatalf("attempt 1 = %v", got)
	}
	if got := payloadPaths(second); !reflect.DeepEqual(got, []string{"a.txt", "new.txt", ""}) {
		t.Fatalf("attempt 2 = %v", got)
	}
	if !strings.Contains(second[0].Patch, "-attempt 1") || !strings.Contains(second[0].Patch, "+attempt 2") {
		t.Fatalf("attempt 2 must diff from attempt 1's result: %q", second[0].Patch)
	}
	if last := second[len(second)-1]; last.Files != 2 || last.DiagnosticCode != "" {
		t.Fatalf("summary = %+v", last)
	}
	if _, statErr := os.Stat(sessions[0].ScratchDirForTest()); !os.IsNotExist(statErr) {
		t.Fatal("scratch must be gone after the run")
	}
}

func payloadPaths(ps []events.AttemptDiffPayload) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Path)
	}
	return out
}

// shape is everything capture must not influence.
type shape struct {
	State    run.State
	Nodes    []run.NodeState
	Types    []string
	Outcomes []string
	Calls    int
}

func shapeOf(r run.Result, a *loopAdapter) shape {
	s := shape{State: r.State, Calls: len(a.calls)}
	for _, n := range r.Nodes {
		s.Nodes = append(s.Nodes, n.State)
	}
	for _, e := range r.Events {
		if e.Type == events.AttemptDiff {
			continue
		}
		s.Types = append(s.Types, string(e.Type)+":"+e.NodeID)
		if e.Type == events.AttemptFinished {
			s.Outcomes = append(s.Outcomes, e.Payload.(events.AttemptFinishedPayload).Outcome)
		}
	}
	return s
}

type codedError struct{ code string }

func (e codedError) Error() string          { return "boom" }
func (e codedError) DiagnosticCode() string { return e.code }

func TestAttemptDiffNeverAltersLoopSemantics(t *testing.T) {
	dir := isoRepo(t)
	baseAdapter := redThenGreen(t, nil)
	base, err := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(baseAdapter), diffOptions(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := shapeOf(base, baseAdapter)
	cases := map[string]*fakeDiffer{
		"capture ok":      {capture: runtime.AttemptCapture{}},
		"before fails":    {snapErr: func(n int) error { return codedError{events.DiffTimeout} }},
		"after fails":     {snapErr: func(n int) error { return map[bool]error{true: codedError{events.DiffGitFailed}}[n%2 == 0] }},
		"diff fails":      {diffErr: errors.New("anything")},
		"invalid payload": {capture: runtime.AttemptCapture{Files: []events.AttemptDiffPayload{{Path: "../escape", Change: "added"}}}},
	}
	for name, fake := range cases {
		t.Run(name, func(t *testing.T) {
			a := redThenGreen(t, nil)
			r, err := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(a), diffOptions(fake))
			if err != nil {
				t.Fatal(err)
			}
			if got := shapeOf(r, a); !reflect.DeepEqual(got, want) {
				t.Fatalf("capture changed execution:\n got %+v\nwant %+v", got, want)
			}
			if fake.opened != fake.closed {
				t.Fatalf("opened %d closed %d", fake.opened, fake.closed)
			}
			for _, e := range diffEvents(r) {
				if err := e.Payload.(events.AttemptDiffPayload).Validate(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	t.Run("failure carries the diagnostic code", func(t *testing.T) {
		fake := &fakeDiffer{snapErr: func(int) error { return codedError{events.DiffTimeout} }}
		r, _ := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(redThenGreen(t, nil)), diffOptions(fake))
		got := diffEvents(r)
		if len(got) == 0 {
			t.Fatal("expected summary events")
		}
		p := got[0].Payload.(events.AttemptDiffPayload)
		if p.Path != "" || p.DiagnosticCode != events.DiffTimeout || p.OmittedReason != events.DiffOmitUnreadable {
			t.Fatalf("summary = %+v", p)
		}
	})
}

func TestAttemptDiffForcedRealGitFailure(t *testing.T) {
	edit := func(ws string, attempt int) {
		if attempt == 1 {
			_ = os.RemoveAll(filepath.Join(ws, ".git"))
		}
	}
	dirA, dirB := isoRepo(t), isoRepo(t)
	plainAdapter := redThenGreen(t, edit)
	plain, err := runtime.Run(context.Background(), repoGraph(dirA, 3), nil, compose(plainAdapter), diffOptions(nil))
	if err != nil {
		t.Fatal(err)
	}
	capturedAdapter := redThenGreen(t, edit)
	captured, err := runtime.Run(context.Background(), repoGraph(dirB, 3), nil, compose(capturedAdapter), diffOptions(realDiffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shapeOf(plain, plainAdapter), shapeOf(captured, capturedAdapter)) {
		t.Fatalf("a capture failure changed the run:\n%+v\n%+v", shapeOf(plain, plainAdapter), shapeOf(captured, capturedAdapter))
	}
	got := diffEvents(captured)
	if len(got) == 0 || got[0].Payload.(events.AttemptDiffPayload).DiagnosticCode == "" {
		t.Fatalf("expected a diagnostic summary, got %d events", len(got))
	}
}

func TestAttemptDiffSecretNeverInAnyEventOrPersistedLine(t *testing.T) {
	const secret = "TOPSECRET-VALUE"
	dir := isoRepo(t)
	a := redThenGreen(t, func(ws string, attempt int) {
		isoWrite(t, ws, "cfg.txt", "line1 "+secret+"\nsplit "+secret[:7]+"\n"+secret[7:]+"\n")
		isoWrite(t, ws, "name-"+secret+".txt", secret+"\n")
	})
	live := make(chan events.Event, 4096)
	var persisted []events.Event
	options := diffOptions(realDiffer{})
	options.LogRedactions = []string{secret}
	options.LiveEvents = live
	options.Persist = func(e events.Event) error { persisted = append(persisted, e); return nil }
	r, err := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(a), options)
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("%v %+v", err, r.Failure)
	}
	close(live)
	var liveEvents []events.Event
	for e := range live {
		liveEvents = append(liveEvents, e)
	}
	for name, evs := range map[string][]events.Event{"result": r.Events, "live": liveEvents, "persisted": persisted} {
		raw, _ := json.Marshal(evs)
		if strings.Contains(string(raw), secret) {
			t.Errorf("secret leaked into %s events", name)
		}
	}
	found := false
	for _, e := range diffEvents(r) {
		if p := e.Payload.(events.AttemptDiffPayload); p.Redacted {
			found = true
		}
	}
	if !found {
		t.Fatal("expected redacted: true on a file that carried the secret")
	}
}

func TestAttemptDiffSkippedWhenProducerErrors(t *testing.T) {
	dir := isoRepo(t)
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{}, errors.New("provider failed")
	}
	fake := &fakeDiffer{}
	r, err := runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(a), diffOptions(fake))
	if err != nil || r.State != run.RunFailed {
		t.Fatalf("%v %+v", err, r)
	}
	if len(diffEvents(r)) != 0 || fake.diffs != 0 {
		t.Fatalf("a failed producer must not be diffed: events=%d diffs=%d", len(diffEvents(r)), fake.diffs)
	}
}

func TestAttemptDiffSkippedWhenContextCancelled(t *testing.T) {
	dir := isoRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		cancel()
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true}`)}, nil
	}
	fake := &fakeDiffer{}
	_, _ = runtime.Run(ctx, repoGraph(dir, 3), nil, compose(a), diffOptions(fake))
	if fake.diffs != 0 || fake.opened != fake.closed {
		t.Fatalf("diffs=%d opened=%d closed=%d", fake.diffs, fake.opened, fake.closed)
	}
}

func TestAttemptDiffOnlyForLoopAgentProducers(t *testing.T) {
	dir := isoRepo(t)
	fake := &fakeDiffer{capture: runtime.AttemptCapture{}}
	r, err := runtime.Run(context.Background(), repoGraph(dir, 1), nil, compose(redThenGreen(t, nil)), diffOptions(fake))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range diffEvents(r) {
		if e.NodeID != "work" {
			t.Fatalf("attempt_diff on %q", e.NodeID)
		}
	}
	if fake.snapshots == 0 || fake.snapshots > 2 {
		t.Fatalf("only the loop Agent is snapshotted, got %d snapshots", fake.snapshots)
	}
}

func TestAttemptDiffOpenFailureIsPreflightErrorBeforeRunStarted(t *testing.T) {
	fake := &fakeDiffer{openErr: errors.New("no scratch")}
	r, err := runtime.Run(context.Background(), repoGraph(isoRepo(t), 1), nil, compose(newLoopAdapter()), diffOptions(fake))
	var pre *run.PreflightError
	if !errors.As(err, &pre) || len(r.Events) != 0 {
		t.Fatalf("err=%v events=%d", err, len(r.Events))
	}
}

func TestAttemptDiffSessionClosedOnEveryExitPath(t *testing.T) {
	dir := isoRepo(t)
	t.Run("success", func(t *testing.T) {
		fake := &fakeDiffer{}
		_, _ = runtime.Run(context.Background(), repoGraph(dir, 3), nil, compose(redThenGreen(t, nil)), diffOptions(fake))
		if fake.opened != 1 || fake.closed != 1 {
			t.Fatalf("%d/%d", fake.opened, fake.closed)
		}
	})
	t.Run("failure", func(t *testing.T) {
		fake := &fakeDiffer{}
		_, _ = runtime.Run(context.Background(), repoGraph(dir, 1), nil, compose(newLoopAdapter()), diffOptions(fake))
		if fake.opened != 1 || fake.closed != 1 {
			t.Fatalf("%d/%d", fake.opened, fake.closed)
		}
	})
	t.Run("real scratch is removed", func(t *testing.T) {
		var sessions []*attemptdiff.Recorder
		a := redThenGreen(t, func(ws string, _ int) { isoWrite(t, ws, "untracked.txt", "u\n") })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, _ = runtime.Run(ctx, repoGraph(dir, 3), nil, compose(a), diffOptions(realDiffer{&sessions}))
		if len(sessions) != 1 {
			t.Fatal("expected one session")
		}
		if _, err := os.Stat(sessions[0].ScratchDirForTest()); !os.IsNotExist(err) {
			t.Fatal("scratch survived the run")
		}
	})
}

func TestAttemptDiffUnderConcurrentLoopNodes(t *testing.T) {
	dir := isoRepo(t)
	d := repoGraph(dir, 3)
	for _, id := range []string{"w2", "w3", "w4"} {
		n := loopDefinition(3).Nodes["work"]
		n.Config["workspace"] = dir
		d.Nodes[id] = n
	}
	options := diffOptions(realDiffer{})
	options.MaxConcurrency = 4
	a := redThenGreen(t, func(ws string, attempt int) {})
	r, err := runtime.Run(context.Background(), d, nil, compose(a), options)
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("%v %+v", err, r.Failure)
	}
	summaries := 0
	for _, e := range diffEvents(r) {
		if e.Payload.(events.AttemptDiffPayload).Path == "" {
			summaries++
		}
	}
	if summaries != len(a.calls) {
		t.Fatalf("one summary per attempt: %d summaries for %d attempts", summaries, len(a.calls))
	}
}

func TestAttemptDiffIsolatedWorktreeOrderAndCleanBase(t *testing.T) {
	src := isoRepo(t)
	a, _ := editAdapter(t, func(t *testing.T, in agent.AgentExecutionInput, _ int) bool {
		isoWrite(t, in.Workspace, in.Task+".txt", "edited\n")
		return true
	})
	options := gitOptions(newGitWorkspaces(t), "")
	options.AttemptDiffs = realDiffer{}
	r, err := runtime.Run(context.Background(), unitGraph(src, []string{"a"}, "a"), nil, compose(a), options)
	if err != nil || r.State != run.RunSucceeded {
		t.Fatalf("%v %+v", err, r.Failure)
	}
	var order []string
	for _, e := range r.Events {
		switch e.Type {
		case events.AttemptDiff, events.AttemptFinished, events.WorkspaceIntegrated:
			order = append(order, string(e.Type))
		}
	}
	want := "attempt_diff,attempt_diff,attempt_finished,workspace_integrated"
	if strings.Join(order, ",") != want {
		t.Fatalf("order = %v", order)
	}
	file := diffEvents(r)[0].Payload.(events.AttemptDiffPayload)
	if file.Path != "a.txt" || file.Change != events.DiffModified {
		t.Fatalf("the diff is taken inside the worktree from a clean base: %+v", file)
	}
}
