package runtime_test

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func loopTimeoutOptions() runtime.Options {
	return runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}}
}

func outcomes(r run.Result) []string {
	var got []string
	for _, e := range r.Events {
		if e.Type == events.AttemptFinished {
			got = append(got, e.Payload.(events.AttemptFinishedPayload).Outcome)
		}
	}
	return got
}

// A timed-out producer attempt consumes exactly one attempt; a later attempt
// can still be accepted and gets no feedback (F3.4).
func TestLoopTimedOutAttemptConsumesOneAttemptThenAccepts(t *testing.T) {
	defer runtime.SetTimeoutForTest(20*time.Millisecond, time.Second)()
	a := newLoopAdapter()
	a.produce = func(ctx context.Context, in agent.AgentExecutionInput, n int) (agent.AgentExecutionOutput, error) {
		if n == 1 {
			<-ctx.Done()
			return agent.AgentExecutionOutput{}, ctx.Err()
		}
		if in.Feedback != nil {
			t.Error("a timed-out attempt must not produce feedback")
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"ok":true}`), ChangedFiles: []agent.ChangedFile{}}, nil
	}
	timeout := 1
	def := loopDefinition(3)
	work := def.Nodes["work"]
	work.TimeoutSeconds = &timeout
	def.Nodes["work"] = work
	down := def.Nodes["down"]
	down.Type = "shell" // "test" is not an allowed node type for validation
	def.Nodes["down"] = down
	r, err := runtime.Run(context.Background(), def, nil, compose(a), loopTimeoutOptions())
	if err != nil || r.State != run.RunSucceeded || len(a.calls) != 2 {
		t.Fatalf("%+v %v calls=%d", r.State, err, len(a.calls))
	}
	if got := outcomes(r); len(got) != 2 || got[0] != "timeout" || got[1] != "accepted" {
		t.Fatalf("outcomes = %v", got)
	}
}

// Exhaustion ends the node failed/timeout and never exceeds max_attempts.
func TestLoopTimeoutExhaustionFailsWithTimeoutKind(t *testing.T) {
	defer runtime.SetTimeoutForTest(20*time.Millisecond, time.Second)()
	a := newLoopAdapter()
	a.produce = func(ctx context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		<-ctx.Done()
		return agent.AgentExecutionOutput{}, ctx.Err()
	}
	timeout := 1
	def := loopDefinition(2)
	work := def.Nodes["work"]
	work.TimeoutSeconds = &timeout
	def.Nodes["work"] = work
	down := def.Nodes["down"]
	down.Type = "shell" // "test" is not an allowed node type for validation
	def.Nodes["down"] = down
	r, err := runtime.Run(context.Background(), def, nil, compose(a), loopTimeoutOptions())
	if err != nil || r.State != run.RunFailed || len(a.calls) != 2 {
		t.Fatalf("%+v %v calls=%d", r.State, err, len(a.calls))
	}
	if r.Failure == nil || r.Failure.Kind != run.FailureTimeout || r.Reason != run.CancelNodeTimeout {
		t.Fatalf("failure = %+v reason %q", r.Failure, r.Reason)
	}
	if got := outcomes(r); len(got) != 2 || got[0] != "timeout" || got[1] != "timeout" {
		t.Fatalf("outcomes = %v", got)
	}
}
