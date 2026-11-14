package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

type providerFunc func(context.Context, ApprovalRequest) (Decision, error)

func (f providerFunc) Decide(ctx context.Context, r ApprovalRequest) (Decision, error) {
	return f(ctx, r)
}

func humanDef(extra map[string]graph.NodeDefinition) *graph.Definition {
	nodes := map[string]graph.NodeDefinition{
		"gate": {Type: "human", Config: map[string]any{"prompt": "ship it?"}, Outputs: map[string]graph.PortDefinition{"decision": {Type: graph.ValueTypeString, Required: true}}},
	}
	for id, n := range extra {
		nodes[id] = n
	}
	return definition(nodes)
}

func humanEvents(result RunResult) (requested, decided []events.Event) {
	for _, e := range result.Events {
		switch e.Type {
		case events.ApprovalRequested:
			requested = append(requested, e)
		case events.ApprovalDecided:
			decided = append(decided, e)
		}
	}
	return
}

func noopExecutor() Executor {
	return executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return map[string]graph.Value{}, nil
	})
}

func TestHumanApprovePublishesDecisionAndRequestPrecedesDecision(t *testing.T) {
	def := humanDef(map[string]graph.NodeDefinition{
		"after": {Type: "shell", Inputs: map[string]graph.PortDefinition{"decision": {Type: graph.ValueTypeString, Required: true}}},
	})
	def.Edges = []graph.EdgeDefinition{{From: "gate.outputs.decision", To: "after.inputs.decision"}}
	requestSeen := make(chan struct{})
	provider := providerFunc(func(ctx context.Context, r ApprovalRequest) (Decision, error) {
		// The decision is withheld until a live consumer has received the
		// request, proving the wait is observable before any decision exists.
		select {
		case <-requestSeen:
			return Decision{Approve: true, Comment: "lgtm"}, nil
		case <-ctx.Done():
			return Decision{}, ctx.Err()
		}
	})
	ch := make(chan events.Event, 256)
	done := make(chan struct{})
	go func() {
		var once sync.Once
		for e := range ch {
			if e.Type == events.ApprovalRequested {
				once.Do(func() { close(requestSeen) })
			}
		}
		close(done)
	}()
	ran := 0
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "after" {
			ran++
			if s, _ := in["decision"].AsString(); s != "approved" {
				t.Errorf("decision input %q", s)
			}
		}
		return map[string]graph.Value{}, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{MaxConcurrency: 2, Decisions: provider, LiveEvents: ch})
	close(ch)
	<-done
	if err != nil || result.State != RunSucceeded || ran != 1 {
		t.Fatalf("err=%v state=%s ran=%d", err, result.State, ran)
	}
	req, dec := humanEvents(result)
	if len(req) != 1 || len(dec) != 1 || req[0].Sequence >= dec[0].Sequence || req[0].NodeRunID != dec[0].NodeRunID {
		t.Fatalf("events req=%d dec=%d", len(req), len(dec))
	}
	p := dec[0].Payload.(events.ApprovalDecidedPayload)
	if p.Decision != "approved" || p.Comment != "lgtm" {
		t.Fatalf("%+v", p)
	}
}

func TestHumanRejectFailsNodeSkipsDependentsWithSafeCode(t *testing.T) {
	def := humanDef(map[string]graph.NodeDefinition{
		"after": {Type: "shell", Inputs: map[string]graph.PortDefinition{"decision": {Type: graph.ValueTypeString, Required: true}}},
	})
	def.Edges = []graph.EdgeDefinition{{From: "gate.outputs.decision", To: "after.inputs.decision"}}
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "after" {
			t.Error("dependent of a rejected human node must not run")
		}
		return map[string]graph.Value{}, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{Decisions: providerFunc(func(context.Context, ApprovalRequest) (Decision, error) {
		return Decision{Approve: false, Comment: "no"}, nil
	})})
	if err != nil || result.State != RunFailed || result.Failure == nil || result.Failure.Kind != FailureRejected {
		t.Fatalf("err=%v result=%+v", err, result.Failure)
	}
	if got := nodeResult(t, result, "after").State; got != NodeSkipped {
		t.Fatalf("after=%s", got)
	}
	var codes []string
	for _, e := range result.Events {
		if p, ok := e.Payload.(events.NodeFinishedPayload); ok && e.NodeID == "gate" {
			codes = p.DiagnosticCodes
		}
	}
	if len(codes) != 1 || codes[0] != "human_rejected" {
		t.Fatalf("codes=%v", codes)
	}
}

func TestHumanWaitSharesBatchWithIndependentNode(t *testing.T) {
	// Deterministic overlap proof: the decision is withheld until the sibling
	// node has finished, so both must be in flight in the same batch.
	siblingDone := make(chan struct{})
	def := humanDef(map[string]graph.NodeDefinition{"sibling": {Type: "shell"}})
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "sibling" {
			close(siblingDone)
		}
		return map[string]graph.Value{}, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{MaxConcurrency: 2, Decisions: providerFunc(func(ctx context.Context, _ ApprovalRequest) (Decision, error) {
		select {
		case <-siblingDone:
			return Decision{Approve: true}, nil
		case <-ctx.Done():
			return Decision{}, ctx.Err()
		}
	})})
	if err != nil || result.State != RunSucceeded {
		t.Fatalf("err=%v state=%s (a sequential schedule would deadlock here)", err, result.State)
	}
}

func TestHumanCancellationWhileWaitingEndsRunWithoutDecisionEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	provider := providerFunc(func(ctx context.Context, _ ApprovalRequest) (Decision, error) {
		close(waiting)
		<-ctx.Done()
		return Decision{}, ctx.Err()
	})
	go func() { <-waiting; cancel() }()
	result, err := Run(ctx, humanDef(nil), nil, noopExecutor(), Options{Decisions: provider})
	if err != nil || result.State != RunCancelled {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if got := nodeResult(t, result, "gate").State; got != NodeCancelled {
		t.Fatalf("gate=%s", got)
	}
	req, dec := humanEvents(result)
	if len(req) != 1 || len(dec) != 0 {
		t.Fatalf("req=%d dec=%d", len(req), len(dec))
	}
}

func TestHumanWithoutProviderFailsPreflightNamingNode(t *testing.T) {
	_, err := Run(context.Background(), humanDef(nil), nil, noopExecutor(), Options{})
	var pre *PreflightError
	if !errors.As(err, &pre) || len(pre.Diagnostics.Diagnostics) != 1 || pre.Diagnostics.Diagnostics[0].Location.NodeID != "gate" || pre.Diagnostics.Diagnostics[0].Code != "E_HUMAN_NO_PROVIDER" {
		t.Fatalf("err=%v", err)
	}
}

func TestHumanProviderFailureIsFatalAndRedactsNothingRaw(t *testing.T) {
	result, err := Run(context.Background(), humanDef(nil), nil, noopExecutor(), Options{Decisions: providerFunc(func(context.Context, ApprovalRequest) (Decision, error) {
		return Decision{}, errors.New("secret-token leaked in provider error")
	})})
	if err != nil || result.State != RunFailed {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	for _, e := range result.Events {
		if strings.Contains(string(mustJSON(t, e)), "secret-token") {
			t.Fatal("provider error text must never enter an event")
		}
	}
}

func TestHumanPromptContextAndCommentAreRedactedAndBounded(t *testing.T) {
	def := humanDef(nil)
	n := def.Nodes["gate"]
	n.Config = map[string]any{"prompt": "token=SUPERSECRET " + strings.Repeat("p", 900)}
	def.Nodes["gate"] = n
	result, err := Run(context.Background(), def, nil, noopExecutor(), Options{LogRedactions: []string{"SUPERSECRET"}, Decisions: providerFunc(func(context.Context, ApprovalRequest) (Decision, error) {
		return Decision{Approve: true, Comment: "SUPERSECRET " + strings.Repeat("c", 5000)}, nil
	})})
	if err != nil || result.State != RunSucceeded {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	req, dec := humanEvents(result)
	rp := req[0].Payload.(events.ApprovalRequestedPayload)
	dp := dec[0].Payload.(events.ApprovalDecidedPayload)
	if len(rp.Prompt) > events.MaxApprovalPrompt || len(dp.Comment) > events.MaxApprovalComment {
		t.Fatalf("unbounded prompt=%d comment=%d", len(rp.Prompt), len(dp.Comment))
	}
	if strings.Contains(rp.Prompt, "SUPERSECRET") || strings.Contains(dp.Comment, "SUPERSECRET") {
		t.Fatal("configured secret reached an approval event")
	}
}

func mustJSON(t *testing.T, e events.Event) []byte {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
