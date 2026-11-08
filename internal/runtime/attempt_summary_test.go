package runtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

func TestAttemptSummariesRetainRejectedAndAcceptedReferences(t *testing.T) {
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(fmt.Sprintf(`{"ok":%t}`, n == 2))}, nil
	}
	r, err := runtime.Run(context.Background(), loopDefinition(2), nil, compose(a), runtime.Options{LogRedactions: []string{"SECRET"}})
	if err != nil || r.State != runtime.RunSucceeded || len(r.Attempts) != 2 {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	if r.Attempts[0].Outcome != runtime.AttemptRejected || r.Attempts[1].Outcome != runtime.AttemptAccepted {
		t.Fatalf("outcomes=%+v", r.Attempts)
	}
	for _, summary := range r.Attempts {
		if summary.AttemptID == "" || summary.NodeID != "work" || summary.NodeRunID != r.Nodes[1].NodeRunID {
			t.Fatalf("identity=%+v", summary)
		}
		if summary.Check == nil || summary.Check.CheckID != "quality" || summary.Check.Kind != events.CheckJSONPredicate || summary.Check.Verdict == "" || summary.Check.Sequence == 0 {
			t.Fatalf("check reference=%+v", summary.Check)
		}
		if summary.Envelope != nil {
			t.Fatalf("unexpected envelope without explicit units: %+v", summary.Envelope)
		}
	}
	if r.Attempts[0].AttemptID == r.Attempts[1].AttemptID {
		t.Fatalf("attempt identity=%+v node=%+v", r.Attempts, r.Nodes[1])
	}
}

func TestAttemptSummarySurvivesProgressEviction(t *testing.T) {
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(fmt.Sprintf(`{"ok":%t}`, n == 2))}, nil
	}
	e := compose(a)
	base := e
	e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
		if n.Type == "agent" {
			for range 4200 {
				o.Progress(events.Status, events.StatusPayload{Code: "working"})
			}
		}
		return base.ExecuteNodeObserved(ctx, id, n, in, o)
	}
	r, err := runtime.Run(context.Background(), loopDefinition(2), nil, e, runtime.Options{})
	if err != nil || r.State != runtime.RunSucceeded || len(r.Attempts) != 2 {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	if r.ProgressStats.RetainedOmittedEvents == 0 {
		t.Fatalf("expected trace pressure, stats=%+v", r.ProgressStats)
	}
	for _, summary := range r.Attempts {
		if summary.Check == nil || summary.Check.Sequence == 0 || summary.AttemptID == "" || summary.NodeRunID == "" || summary.Outcome == "" {
			t.Fatalf("lost attempt reference: %+v", summary)
		}
	}
}

func TestAttemptSummaryReferencesExplicitUnitEnvelope(t *testing.T) {
	d := loopDefinition(1)
	d.Units = []string{"work"}
	n := d.Nodes["work"]
	n.Config["workspace"] = unitRepo(t)
	n.Config["allowed_paths"] = []string{"src/**"}
	d.Nodes["work"] = n
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true}`)}, nil
	}
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{})
	if err != nil || r.State != runtime.RunSucceeded || len(r.Attempts) != 1 {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	s := r.Attempts[0]
	if s.Check == nil || s.Envelope == nil || s.Envelope.UnitID != "work" || s.Envelope.CheckID != "quality" || s.Envelope.Verdict != events.CheckGreen || s.Envelope.Sequence <= s.Check.Sequence {
		t.Fatalf("envelope reference=%+v check=%+v", s.Envelope, s.Check)
	}
	if r.Nodes[1].AcceptedAttemptID != s.AttemptID {
		t.Fatalf("accepted identity=%q summary=%+v", r.Nodes[1].AcceptedAttemptID, s)
	}
}

func TestCancelledAttemptSummaryHasTerminalOutcome(t *testing.T) {
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
	if err != nil || r.State != runtime.RunCancelled || len(r.Attempts) != 1 || r.Attempts[0].Outcome != runtime.AttemptCancelled || r.Attempts[0].Check == nil {
		t.Fatalf("run=%+v err=%v", r, err)
	}
}

func TestAttemptSummaryCarriesRedactionProvenanceWithoutRawCheckText(t *testing.T) {
	d := loopDefinition(1)
	n := d.Nodes["work"]
	n.Loop.Gate.Config["check_id"] = "SECRET-check"
	d.Nodes["work"] = n
	a := newLoopAdapter()
	a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true}`)}, nil
	}
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{LogRedactions: []string{"SECRET"}})
	if err != nil || r.State != runtime.RunSucceeded || len(r.Attempts) != 1 || r.Attempts[0].Check == nil {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	ref := r.Attempts[0].Check
	if !ref.IdentityRedacted || strings.Contains(ref.CheckID, "SECRET") || ref.CheckID == "" {
		t.Fatalf("unsafe check reference=%+v", ref)
	}
	encoded, err := json.Marshal(r.Attempts)
	if err != nil || strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "matched") {
		t.Fatalf("unsafe summary=%s err=%v", encoded, err)
	}
}

func TestAttemptSummaryBudgetRejectsBeforeDispatch(t *testing.T) {
	boundary := &graph.Definition{Nodes: make(map[string]graph.NodeDefinition)}
	for i := range 409 {
		boundary.Nodes[fmt.Sprintf("node_%d", i)] = graph.NodeDefinition{Loop: &graph.LoopDefinition{MaxAttempts: 10}}
	}
	boundary.Nodes["last"] = graph.NodeDefinition{Loop: &graph.LoopDefinition{MaxAttempts: 6}}
	if err := runtime.ValidateAttemptSummaryBudget(boundary); err != nil {
		t.Fatalf("exact budget rejected: %v", err)
	}
	d := loopDefinition(10)
	base := d.Nodes["work"]
	for i := range 409 {
		d.Nodes[fmt.Sprintf("extra_%d", i)] = base
	}
	if err := runtime.ValidateAttemptSummaryBudget(d); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("budget error=%v", err)
	}
	called := false
	a := newLoopAdapter()
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{BeforeDispatch: func(events.RunID) error { called = true; return nil }})
	if err == nil || !strings.Contains(err.Error(), "E_ATTEMPT_SUMMARY_BUDGET") || r.RunID != "" || called || a.detect != 0 {
		t.Fatalf("oversized graph started: run=%+v err=%v dispatched=%t", r, err, called)
	}
}
