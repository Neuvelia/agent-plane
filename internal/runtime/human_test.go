package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

type providerFunc func(context.Context, run.ApprovalRequest) (run.Decision, error)

func (f providerFunc) Decide(ctx context.Context, r run.ApprovalRequest) (run.Decision, error) {
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

func humanEvents(result run.Result) (requested, decided []events.Event) {
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

// roadmap: F2.10-AC1
func TestHumanApprovePublishesDecisionAndRequestPrecedesDecision(t *testing.T) {
	def := humanDef(map[string]graph.NodeDefinition{
		"after": {Type: "shell", Inputs: map[string]graph.PortDefinition{"decision": {Type: graph.ValueTypeString, Required: true}}},
	})
	def.Edges = []graph.EdgeDefinition{{From: "gate.outputs.decision", To: "after.inputs.decision"}}
	requestSeen := make(chan struct{})
	provider := providerFunc(func(ctx context.Context, _ run.ApprovalRequest) (run.Decision, error) {
		// The decision is withheld until a live consumer has received the
		// request, proving the wait is observable before any decision exists.
		select {
		case <-requestSeen:
			return run.Decision{Approve: true, Comment: "lgtm"}, nil
		case <-ctx.Done():
			return run.Decision{}, ctx.Err()
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2, Decisions: provider, LiveEvents: ch})
	close(ch)
	<-done
	if err != nil || result.State != run.RunSucceeded || ran != 1 {
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

// roadmap: F2.10-AC2
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
		return run.Decision{Approve: false, Comment: "no"}, nil
	})})
	if err != nil || result.State != run.RunFailed || result.Failure == nil || result.Failure.Kind != run.FailureRejected {
		t.Fatalf("err=%v result=%+v", err, result.Failure)
	}
	if got := nodeResult(t, result, "after").State; got != run.NodeSkipped {
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

// roadmap: F2.10-AC2
func TestHumanRejectWithOnRejectContinuePublishesTheRejectionToItsCheck(t *testing.T) {
	// The decision feeds a graph output, one of the two deterministic checks
	// ADR-0043 accepts; validation of the other (a Gate) is proven in
	// internal/adapter/yamlgraph.
	def := humanDef(nil)
	node := def.Nodes["gate"]
	node.Config = map[string]any{"prompt": "ship it?", "on_reject": graph.HumanRejectContinue}
	def.Nodes["gate"] = node
	def.Outputs = map[string]graph.OutputDefinition{"verdict": {From: "gate.outputs.decision"}}
	result, err := Run(context.Background(), def, nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
		return run.Decision{Approve: false, Comment: "no"}, nil
	})})
	if err != nil || result.State != run.RunSucceeded || result.Failure != nil {
		t.Fatalf("err=%v state=%s failure=%+v", err, result.State, result.Failure)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("outputs = %+v", result.Outputs)
	}
	if got, _ := result.Outputs[0].Value.AsString(); got != "rejected" {
		t.Fatalf("graph output verdict = %q, want rejected", got)
	}
	if _, dec := humanEvents(result); len(dec) != 1 || dec[0].Payload.(events.ApprovalDecidedPayload).Decision != "rejected" {
		t.Fatalf("approval_decided must still record the rejection: %+v", dec)
	}
}

// roadmap: F2.10-AC1
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2, Decisions: providerFunc(func(ctx context.Context, _ run.ApprovalRequest) (run.Decision, error) {
		select {
		case <-siblingDone:
			return run.Decision{Approve: true}, nil
		case <-ctx.Done():
			return run.Decision{}, ctx.Err()
		}
	})})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s (a sequential schedule would deadlock here)", err, result.State)
	}
}

func TestHumanCancellationWhileWaitingEndsRunWithoutDecisionEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	provider := providerFunc(func(ctx context.Context, _ run.ApprovalRequest) (run.Decision, error) {
		close(waiting)
		<-ctx.Done()
		return run.Decision{}, ctx.Err()
	})
	go func() { <-waiting; cancel() }()
	result, err := Run(ctx, humanDef(nil), nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: provider})
	if err != nil || result.State != run.RunCancelled {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if got := nodeResult(t, result, "gate").State; got != run.NodeCancelled {
		t.Fatalf("gate=%s", got)
	}
	req, dec := humanEvents(result)
	if len(req) != 1 || len(dec) != 0 {
		t.Fatalf("req=%d dec=%d", len(req), len(dec))
	}
}

func TestHumanWithoutProviderFailsPreflightNamingNode(t *testing.T) {
	_, err := Run(context.Background(), humanDef(nil), nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	var pre *run.PreflightError
	if !errors.As(err, &pre) || len(pre.Diagnostics.Diagnostics) != 1 || pre.Diagnostics.Diagnostics[0].Location.NodeID != "gate" || pre.Diagnostics.Diagnostics[0].Code != "E_HUMAN_NO_PROVIDER" {
		t.Fatalf("err=%v", err)
	}
}

func TestHumanProviderFailureIsFatalAndRedactsNothingRaw(t *testing.T) {
	result, err := Run(context.Background(), humanDef(nil), nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
		return run.Decision{}, errors.New("secret-token leaked in provider error")
	})})
	if err != nil || result.State != run.RunFailed {
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
	result, err := Run(context.Background(), def, nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"SUPERSECRET"}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
		return run.Decision{Approve: true, Comment: "SUPERSECRET " + strings.Repeat("c", 5000)}, nil
	})})
	if err != nil || result.State != run.RunSucceeded {
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

func humanWithSource(secretText string) *graph.Definition {
	def := humanDef(map[string]graph.NodeDefinition{
		"src": {Type: "shell", Outputs: map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString, Required: true}}},
	})
	n := def.Nodes["gate"]
	n.Inputs = map[string]graph.PortDefinition{"summary": {Type: graph.ValueTypeString, Required: true}}
	n.Config = map[string]any{"prompt": "ship?", "context_inputs": []any{"summary"}}
	def.Nodes["gate"] = n
	def.Edges = []graph.EdgeDefinition{{From: "src.outputs.stdout", To: "gate.inputs.summary"}}
	return def
}

func srcExecutor(text string) Executor {
	return executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "src" {
			return map[string]graph.Value{"stdout": graph.NewStringValue(text)}, nil
		}
		return map[string]graph.Value{}, nil
	})
}

// Redaction must run before truncation: a secret that straddles the limit would
// otherwise leave a readable prefix behind.
func TestHumanRedactionHappensBeforeTruncation(t *testing.T) {
	const secret = "QZXSECRETVALUE"
	prompt := strings.Repeat("p", 900) + secret // graph validation already bounds the prompt, so only redaction matters here
	def := humanWithSource("")
	n := def.Nodes["gate"]
	n.Config = map[string]any{"prompt": prompt, "context_inputs": []any{"summary"}}
	def.Nodes["gate"] = n
	context1 := strings.Repeat("c", events.MaxApprovalContextValue-4) + secret // and in a context value
	comment := strings.Repeat("m", events.MaxApprovalComment-4) + secret       // and in the comment
	result, err := Run(context.Background(), def, nil, srcExecutor(context1), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{secret}, Decisions: providerFunc(func(_ context.Context, r run.ApprovalRequest) (run.Decision, error) {
		for _, c := range r.Context {
			if strings.Contains(c.Value, "QZ") {
				t.Errorf("provider saw a secret prefix in context: %q", c.Value[len(c.Value)-12:])
			}
		}
		if strings.Contains(r.Prompt, "QZ") {
			t.Errorf("provider saw a secret prefix in the prompt")
		}
		return run.Decision{Approve: true, Comment: comment}, nil
	})})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	for _, e := range result.Events {
		if strings.Contains(string(mustJSON(t, e)), "QZ") {
			t.Fatalf("a secret fragment reached %s", e.Type)
		}
	}
	req, _ := humanEvents(result)
	if got := req[0].Payload.(events.ApprovalRequestedPayload).Context; len(got) != 1 || got[0].Name != "summary" {
		t.Fatalf("context_inputs was not carried: %+v", got)
	}
}

func TestHumanProviderFailureFailsNodeWithSafeCodeAndNoDecisionEvent(t *testing.T) {
	result, err := Run(context.Background(), humanDef(nil), nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
		return run.Decision{}, errors.New("boom")
	})})
	if err != nil || result.State != run.RunFailed {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if got := nodeResult(t, result, "gate").State; got != run.NodeFailed {
		t.Fatalf("gate=%s", got)
	}
	var codes []string
	for _, e := range result.Events {
		if p, ok := e.Payload.(events.NodeFinishedPayload); ok && e.NodeID == "gate" {
			codes = p.DiagnosticCodes
		}
	}
	if len(codes) != 1 || codes[0] != "human_decision_invalid" {
		t.Fatalf("codes=%v", codes)
	}
	req, dec := humanEvents(result)
	if len(req) != 1 || len(dec) != 0 {
		t.Fatalf("a provider failure must close the wait with node_finished only: req=%d dec=%d", len(req), len(dec))
	}
}

// The provider is asked exactly once per human node run, in every outcome.
func TestHumanProviderIsAskedOncePerNodeRun(t *testing.T) {
	for _, approve := range []bool{true, false} {
		var calls int
		var mu sync.Mutex
		result, _ := Run(context.Background(), humanDef(nil), nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			return run.Decision{Approve: approve}, nil
		})})
		_, dec := humanEvents(result)
		if calls != 1 || len(dec) != 1 {
			t.Fatalf("approve=%v calls=%d decided=%d", approve, calls, len(dec))
		}
	}
}

// A decision the provider returned while cancellation arrived is still a fact
// the person acted on: it is recorded and the node ends cancelled.
func TestHumanDecisionReturnedDuringCancellationIsRecorded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := Run(ctx, humanDef(nil), nil, noopExecutor(), Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, Decisions: providerFunc(func(context.Context, run.ApprovalRequest) (run.Decision, error) {
		cancel()
		return run.Decision{Approve: true, Comment: "late"}, nil
	})})
	if err != nil || result.State != run.RunCancelled {
		t.Fatalf("err=%v state=%s", err, result.State)
	}
	if got := nodeResult(t, result, "gate").State; got != run.NodeCancelled {
		t.Fatalf("gate=%s", got)
	}
	_, dec := humanEvents(result)
	if len(dec) != 1 || dec[0].Payload.(events.ApprovalDecidedPayload).Comment != "late" {
		t.Fatalf("the accepted decision was lost: %d events", len(dec))
	}
}
