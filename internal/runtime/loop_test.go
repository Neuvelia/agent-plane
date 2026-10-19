package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/events"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

type loopAdapter struct {
	mu      sync.Mutex
	calls   []agent.AgentExecutionInput
	detect  int
	capable *bool
	produce func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error)
}

func (*loopAdapter) ID() agent.AdapterID { return "fake" }
func (a *loopAdapter) Detect(context.Context) (agent.CapabilityReport, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.detect++
	return agent.CapabilityReport{Available: true, CorrectionFeedback: a.capable}, nil
}
func (a *loopAdapter) Execute(ctx context.Context, in agent.AgentExecutionInput) (agent.AgentExecutionOutput, error) {
	a.mu.Lock()
	a.calls = append(a.calls, in)
	n := len(a.calls)
	a.mu.Unlock()
	return a.produce(ctx, in, n)
}

type composition struct {
	agents     agent.Executor
	hook       func(context.Context, string, graph.NodeDefinition, map[string]graph.Value, runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error)
	downstream func(map[string]graph.Value)
}

func (e composition) ExecuteNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
	v, _, err := e.ExecuteNodeObserved(ctx, id, n, in, runtime.ExecutionObservation{})
	return v, err
}
func (e composition) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	if e.hook != nil {
		return e.hook(ctx, id, n, in, o)
	}
	if n.Type == "gate" {
		return (gate.Executor{}).ExecuteNodeObserved(ctx, id, n, in, o)
	}
	if n.Type == "agent" {
		return e.agents.ExecuteNodeObserved(ctx, id, n, in, o)
	}
	if e.downstream != nil {
		e.downstream(in)
	}
	return nil, nil, nil
}
func (e composition) ValidateNodeConfig(id string, n graph.NodeDefinition) error {
	if n.Type == "gate" {
		return (gate.Executor{}).ValidateNodeConfig(id, n)
	}
	return e.agents.ValidateNodeConfig(id, n)
}
func (e composition) PreflightNode(ctx context.Context, id string, n graph.NodeDefinition) error {
	return e.agents.PreflightNode(ctx, id, n)
}
func loopDefinition(budget int) *graph.Definition {
	return &graph.Definition{SchemaVersion: 0, ID: "loop", Inputs: map[string]graph.InputDefinition{}, Edges: []graph.EdgeDefinition{{From: "work.outputs.raw_metadata", To: "down.inputs.result"}}, Outputs: map[string]graph.OutputDefinition{}, Nodes: map[string]graph.NodeDefinition{
		"work": {Type: "agent", Config: graph.NodeConfig{"adapter": "fake", "task": "original SECRET task", "workspace": "/tmp", "allowed_paths": []string{"src/SECRET"}}, Outputs: map[string]graph.PortDefinition{"raw_metadata": {Type: graph.ValueTypeJSON, Required: true}}, Loop: &graph.LoopDefinition{MaxAttempts: budget, Gate: graph.LoopGateDefinition{Inputs: map[string]graph.PortDefinition{"candidate": {Type: graph.ValueTypeJSON, Required: true}}, Config: graph.NodeConfig{"check_id": "quality", "kind": "json_predicate", "input": "candidate", "field": []string{"ok"}, "operator": "eq", "expected": true}}, Bindings: []graph.LoopBinding{{From: "candidate.outputs.raw_metadata", To: "gate.inputs.candidate"}}}},
		"down": {Type: "test", Inputs: map[string]graph.PortDefinition{"result": {Type: graph.ValueTypeJSON, Required: true}}},
	}}
}
func newLoopAdapter() *loopAdapter {
	yes := true
	return &loopAdapter{capable: &yes, produce: func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":false}`), ChangedFiles: []agent.ChangedFile{}}, nil
	}}
}
func compose(a *loopAdapter) composition {
	return composition{agents: agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"fake": a}}}
}
func TestLoopBudgetsAndTrace(t *testing.T) {
	for _, b := range []int{0, 1, 2, 10} {
		t.Run(string(rune('a'+b)), func(t *testing.T) {
			a := newLoopAdapter()
			r, err := runtime.Run(context.Background(), loopDefinition(b), nil, compose(a), runtime.Options{LogRedactions: []string{"SECRET"}})
			want := b
			if want == 0 {
				want = 3
			}
			if err != nil || r.State != runtime.RunFailed || r.Failure.Kind != runtime.FailureNeedsIntervention || len(a.calls) != want {
				t.Fatalf("%+v %v calls=%d", r, err, len(a.calls))
			}
			var inputs, checks, finishes int
			ids := map[events.AttemptID]bool{}
			for _, e := range r.Events {
				switch e.Type {
				case events.AttemptStarted:
					if ids[e.AttemptID] || e.AttemptID == "" {
						t.Fatal("attempt reused")
					}
					ids[e.AttemptID] = true
				case events.AttemptInput:
					inputs++
				case events.GateCheck:
					checks++
					if len(events.AttemptCheckResults(r.Events, r.RunID, e.NodeRunID, e.AttemptID)) != 1 {
						t.Fatal("lookup")
					}
				case events.AttemptFinished:
					finishes++
					p := e.Payload.(events.AttemptFinishedPayload)
					if finishes == want && p.Outcome != "needs_intervention" {
						t.Fatal(p)
					}
				}
			}
			if inputs != want || checks != want || finishes != want {
				t.Fatal(inputs, checks, finishes)
			}
			encoded, _ := json.Marshal(r.Events)
			if strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "original ") {
				t.Fatal(string(encoded))
			}
			if len(r.Nodes[1].Outputs) != 0 || r.Nodes[0].State != runtime.NodeSkipped {
				t.Fatal(r.Nodes)
			}
		})
	}
}
func TestLoopRedGreenPublicationAndFeedback(t *testing.T) {
	a := newLoopAdapter()
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		if n == 1 && in.Feedback != nil {
			t.Fatal("initial feedback")
		}
		if n == 2 {
			if in.Task != "original SECRET task" || !reflect.DeepEqual(in.Feedback.IntendedScope, []string{"src/SECRET"}) {
				t.Fatal(in)
			}
		}
		raw := `{"ok":false}`
		if n > 1 {
			raw = `{"ok":true}`
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(raw), ChangedFiles: []agent.ChangedFile{}}, nil
	}
	e := compose(a)
	down := 0
	e.downstream = func(in map[string]graph.Value) {
		down++
		b, _ := in["result"].AsJSON()
		if string(b) != `{"ok":true}` {
			t.Fatal(string(b))
		}
	}
	r, err := runtime.Run(context.Background(), loopDefinition(0), nil, e, runtime.Options{LogRedactions: []string{"SECRET"}})
	if err != nil || r.State != runtime.RunSucceeded || len(a.calls) != 2 || down != 1 {
		t.Fatalf("%+v %v calls=%d down=%d", r, err, len(a.calls), down)
	}
	var red events.CheckResult
	for _, ev := range r.Events {
		if p, ok := ev.Payload.(events.CheckResult); ok && p.Verdict == "red" {
			red = p
		}
	}
	f := a.calls[1].Feedback
	if !reflect.DeepEqual(f.CheckResult, red) || f.PreviousAttemptID != a.calls[0].AttemptID || a.calls[0].AttemptID == a.calls[1].AttemptID || a.calls[0].NodeRunID != a.calls[1].NodeRunID {
		t.Fatal(a.calls)
	}
	f.CheckResult.Evidence[0].Path = "MUTATED"
	if red.Evidence[0].Path == "MUTATED" {
		t.Fatal("feedback aliases trace")
	}
}
func TestLoopPreflightBeforeDetection(t *testing.T) {
	for _, mutate := range []func(*graph.Definition){
		func(d *graph.Definition) { n := d.Nodes["work"]; n.Loop.MaxAttempts = 11 },
		func(d *graph.Definition) { n := d.Nodes["work"]; n.Loop.Gate.Config["unknown"] = true },
		func(d *graph.Definition) { n := d.Nodes["work"]; n.Loop.Bindings = nil },
		func(d *graph.Definition) { n := d.Nodes["work"]; delete(n.Config, "allowed_paths") },
		func(d *graph.Definition) { n := d.Nodes["work"]; n.Config["task"] = nil },
	} {
		d := loopDefinition(3)
		mutate(d)
		a := newLoopAdapter()
		_, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{})
		if err == nil || len(a.calls) != 0 || a.detect != 0 {
			t.Fatal(err, a.calls, a.detect)
		}
	}
	for _, capable := range []*bool{nil, new(bool)} {
		a := newLoopAdapter()
		a.capable = capable
		_, err := runtime.Run(context.Background(), loopDefinition(3), nil, compose(a), runtime.Options{})
		if err == nil || len(a.calls) != 0 {
			t.Fatal(err)
		}
	}
}
func TestLoopFatalNeverRetries(t *testing.T) {
	for _, mode := range []string{"provider", "output", "gate-error", "missing", "duplicate", "inconsistent", "producer-check"} {
		t.Run(mode, func(t *testing.T) {
			a := newLoopAdapter()
			e := compose(a)
			base := e
			e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
				if n.Type == "agent" {
					if mode == "provider" {
						a.calls = append(a.calls, agent.AgentExecutionInput{})
						return nil, nil, errors.New("provider process failed")
					}
					if mode == "output" {
						a.calls = append(a.calls, agent.AgentExecutionInput{})
						return nil, nil, nil
					}
					if mode == "producer-check" {
						o.Check(events.GateCheckPayload{})
					}
					return base.ExecuteNodeObserved(ctx, id, n, in, o)
				}
				if n.Type == "gate" {
					if mode == "gate-error" {
						return nil, nil, errors.New("gate process failed")
					}
					if mode == "missing" {
						return nil, nil, nil
					}
					if mode == "inconsistent" {
						_, _, _ = base.ExecuteNodeObserved(ctx, id, n, in, o)
						return nil, nil, nil
					}
					if mode == "duplicate" {
						_, _, _ = base.ExecuteNodeObserved(ctx, id, n, in, o)
						return base.ExecuteNodeObserved(ctx, id, n, in, o)
					}
				}
				return base.ExecuteNodeObserved(ctx, id, n, in, o)
			}
			r, err := runtime.Run(context.Background(), loopDefinition(3), nil, e, runtime.Options{})
			if err != nil || r.State != runtime.RunFailed || len(a.calls) != 1 || r.Failure.Kind == runtime.FailureNeedsIntervention {
				t.Fatalf("%+v %v calls=%d", r, err, len(a.calls))
			}
		})
	}
}

func TestLoopCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"before", "produce", "before-check", "check", "between"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := newLoopAdapter()
			e := compose(a)
			d := loopDefinition(3)
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			base := e
			if stage == "before" {
				cancel()
			} else if stage == "produce" {
				a.produce = func(ctx context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
					close(entered)
					<-ctx.Done()
					return agent.AgentExecutionOutput{}, ctx.Err()
				}
			} else if stage == "before-check" {
				a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
					cancel()
					return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":false}`)}, nil
				}
			} else {
				e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
					if id == "peer" {
						close(entered)
						<-release
						return nil, nil, nil
					}
					if n.Type == "gate" {
						if stage == "check" {
							close(entered)
							<-ctx.Done()
							return nil, nil, ctx.Err()
						}
						v, p, err := base.ExecuteNodeObserved(ctx, id, n, in, o)
						once.Do(func() { close(release) })
						return v, p, err
					}
					return base.ExecuteNodeObserved(ctx, id, n, in, o)
				}
				if stage == "between" {
					// Hold the peer until cancellation after the first red check. The batch
					// barrier prevents a correction dispatch while the peer is outstanding.
					gateDone := release
					peerRelease := make(chan struct{})
					release = peerRelease
					e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
						if id == "peer" {
							<-peerRelease
							return nil, nil, nil
						}
						v, p, err := base.ExecuteNodeObserved(ctx, id, n, in, o)
						if n.Type == "gate" {
							close(gateDone)
						}
						return v, p, err
					}
					entered = gateDone
					d.Nodes["peer"] = graph.NodeDefinition{Type: "test"}
				}
			}
			type outcome struct {
				r   runtime.RunResult
				err error
			}
			done := make(chan outcome, 1)
			go func() {
				r, err := runtime.Run(ctx, d, nil, e, runtime.Options{MaxConcurrency: 2})
				done <- outcome{r, err}
			}()
			if stage == "produce" || stage == "check" || stage == "between" {
				<-entered
				cancel()
				if stage == "between" {
					close(release)
				}
			}
			result := <-done
			if result.err != nil || result.r.State != runtime.RunCancelled {
				t.Fatalf("%+v %v", result.r, result.err)
			}
			want := 1
			if stage == "before" {
				want = 0
			}
			if len(a.calls) != want {
				t.Fatal("calls", len(a.calls))
			}
			for _, n := range result.r.Nodes {
				if n.State == runtime.NodeRunning {
					t.Fatal("running after cancellation")
				}
			}
		})
	}
}
func TestLoopLateCallbacksAndCurrentAttemptCorrelation(t *testing.T) {
	a := newLoopAdapter()
	e := compose(a)
	base := e
	var late []func()
	e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if n.Type == "agent" {
			o.Log("stdout", []byte("SECRET\n"))
			o.Progress(events.TextDelta, events.TextDeltaPayload{Text: "SECRET"})
			late = append(late, func() {
				o.Log("stdout", []byte("LATE"))
				o.Progress(events.TextDelta, events.TextDeltaPayload{Text: "LATE"})
				o.Check(events.GateCheckPayload{})
			})
		}
		return base.ExecuteNodeObserved(ctx, id, n, in, o)
	}
	r, err := runtime.Run(context.Background(), loopDefinition(2), nil, e, runtime.Options{LogRedactions: []string{"SECRET"}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(r.Events)
	for _, callback := range late {
		callback()
	}
	after, _ := json.Marshal(r.Events)
	if string(before) != string(after) {
		t.Fatal("late callback changed trace")
	}
	counts := map[events.AttemptID]int{}
	for _, ev := range r.Events {
		if ev.Type == events.LogChunk || ev.Type == events.TextDelta || ev.Type == events.NodeEvidence {
			if ev.AttemptID == "" {
				t.Fatal("missing current attempt")
			}
			counts[ev.AttemptID]++
		}
	}
	if len(counts) != 2 || strings.Contains(string(before), "SECRET") || strings.Contains(string(before), "LATE") {
		t.Fatal(string(before))
	}
}
func TestLoopStaticValidationAllNodesBeforeDetect(t *testing.T) {
	a := newLoopAdapter()
	d := loopDefinition(3)
	n := d.Nodes["work"]
	n.Config = graph.NodeConfig{"adapter": "fake", "task": "task", "workspace": "relative", "allowed_paths": []string{}}
	d.Nodes["zbad"] = n
	_, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{})
	if err == nil || a.detect != 0 {
		t.Fatal(err, a.detect)
	}
}
func TestLoopCompletedPeerNeverRerunsAndBarrier(t *testing.T) {
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		if n == 1 {
			return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":false}`)}, nil
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true}`)}, nil
	}
	e := compose(a)
	base := e
	peerDone := make(chan struct{})
	redDone := make(chan struct{})
	release := make(chan struct{})
	count := 0
	e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if id == "peer" {
			count++
			<-release
			close(peerDone)
			return nil, nil, nil
		}
		if n.Type == "agent" && o.Feedback != nil {
			select {
			case <-peerDone:
			default:
				t.Error("correction crossed batch barrier")
			}
		}
		v, p, err := base.ExecuteNodeObserved(ctx, id, n, in, o)
		if n.Type == "gate" && err != nil {
			close(redDone)
		}
		return v, p, err
	}
	d := loopDefinition(3)
	d.Nodes["peer"] = graph.NodeDefinition{Type: "test"}
	done := make(chan runtime.RunResult, 1)
	go func() {
		r, err := runtime.Run(context.Background(), d, nil, e, runtime.Options{MaxConcurrency: 2})
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()
	<-redDone
	close(release)
	r := <-done
	if r.State != runtime.RunSucceeded || count != 1 || len(a.calls) != 2 {
		t.Fatal(r.State, count, len(a.calls))
	}
}
func TestLoopSanitizedFeedbackIsDetachedRetainedResult(t *testing.T) {
	a := newLoopAdapter()
	e := compose(a)
	base := e
	e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if n.Type == "gate" {
			check := o.Check
			o.Check = func(p events.GateCheckPayload) {
				p.Reason = "SECRET reason"
				p.Evidence[0].Path = "SECRET/path"
				p.Evidence[0].Reference = &events.EvidenceReference{Kind: "file", State: "known", Identifier: "SECRET/file"}
				check(p)
				p.Evidence[0].Path = "MUTATED"
				p.Evidence[0].Reference.Identifier = "MUTATED"
			}
		}
		return base.ExecuteNodeObserved(ctx, id, n, in, o)
	}
	r, err := runtime.Run(context.Background(), loopDefinition(2), nil, e, runtime.Options{LogRedactions: []string{"SECRET"}})
	if err != nil || len(a.calls) != 2 {
		t.Fatal(err, r)
	}
	checks := events.CheckResults(r.Events, r.RunID, a.calls[0].NodeRunID)
	if len(checks) != 2 || !reflect.DeepEqual(checks[0], a.calls[1].Feedback.CheckResult) {
		t.Fatal(checks, a.calls[1].Feedback)
	}
	encoded, _ := json.Marshal(r.Events)
	if strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "MUTATED") {
		t.Fatal(string(encoded))
	}
	a.calls[1].Feedback.CheckResult.Evidence[0].Reference.Identifier = "NEW"
	if checks[0].Evidence[0].Reference.Identifier == "NEW" {
		t.Fatal("feedback mutable alias")
	}
}
func TestLoopCancellationDoesNotHideRacingProviderFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		cancel()
		return agent.AgentExecutionOutput{}, errors.New("unrelated provider failure")
	}
	r, err := runtime.Run(ctx, loopDefinition(3), nil, compose(a), runtime.Options{})
	if err != nil || r.State != runtime.RunFailed || r.Failure.Kind != runtime.FailureExecutor || !strings.Contains(r.Failure.Err.Error(), "unrelated") {
		t.Fatal(r, err)
	}
}
func TestLoopRejectsWrongCheckedScope(t *testing.T) {
	a := newLoopAdapter()
	e := compose(a)
	base := e
	e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if n.Type == "gate" {
			check := o.Check
			o.Check = func(p events.GateCheckPayload) { p.Scope.CheckedInputs = []string{"other"}; check(p) }
		}
		return base.ExecuteNodeObserved(ctx, id, n, in, o)
	}
	r, err := runtime.Run(context.Background(), loopDefinition(3), nil, e, runtime.Options{})
	if err != nil || r.State != runtime.RunFailed || len(a.calls) != 1 {
		t.Fatal(r, err)
	}
}
func TestLoopCancellationAfterFinalRedIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newLoopAdapter()
	e := compose(a)
	base := e
	e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		v, p, err := base.ExecuteNodeObserved(ctx, id, n, in, o)
		if n.Type == "gate" {
			cancel()
		}
		return v, p, err
	}
	r, err := runtime.Run(ctx, loopDefinition(1), nil, e, runtime.Options{})
	if err != nil || r.State != runtime.RunCancelled || len(a.calls) != 1 {
		t.Fatal(r, err)
	}
	for _, ev := range r.Events {
		if p, ok := ev.Payload.(events.AttemptFinishedPayload); ok && p.Outcome != "cancelled" {
			t.Fatal(p)
		}
	}
}
func TestLoopAttemptInputBoundsAndReferenceOmission(t *testing.T) {
	a := newLoopAdapter()
	d := loopDefinition(1)
	n := d.Nodes["work"]
	scope := []string{strings.Repeat("x", 513)}
	for range 100 {
		scope = append(scope, strings.Repeat("s", 64))
	}
	n.Config["allowed_paths"] = scope
	n.Config["context_refs"] = []string{"PRIVATE_CONTEXT"}
	n.Config["environment_refs"] = []string{"PRIVATE_ENV"}
	d.Nodes["work"] = n
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range r.Events {
		if p, ok := ev.Payload.(events.AttemptInputPayload); ok {
			if !p.ScopeOmitted || !p.ScopePresent || !p.ContextRefsPresent || !p.EnvironmentRefsPresent || !p.ReferencesOmitted || len(p.IntendedScope) != 16 || len(p.TaskFingerprint) != 64 {
				t.Fatal(p)
			}
			b, _ := json.Marshal(p)
			if len(b) > 8192 || strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "original") {
				t.Fatal(string(b))
			}
		}
	}
	if !reflect.DeepEqual(a.calls[0].AllowedPaths, scope) {
		t.Fatal("trace bound changed original provider scope")
	}
}
func TestLoopInitialGreenAcceptsOnce(t *testing.T) {
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true}`)}, nil
	}
	e := compose(a)
	down := 0
	e.downstream = func(map[string]graph.Value) { down++ }
	r, err := runtime.Run(context.Background(), loopDefinition(3), nil, e, runtime.Options{})
	if err != nil || r.State != runtime.RunSucceeded || len(a.calls) != 1 || down != 1 || a.calls[0].Feedback != nil {
		t.Fatal(r, err)
	}
}

type legacyLoopExecutor struct{ calls int }

func (e *legacyLoopExecutor) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	e.calls++
	return nil, nil
}
func TestLoopRequiresPreflightCompositionBeforeProduce(t *testing.T) {
	e := &legacyLoopExecutor{}
	_, err := runtime.Run(context.Background(), loopDefinition(3), nil, e, runtime.Options{})
	if err == nil || e.calls != 0 {
		t.Fatal(err, e.calls)
	}
}
