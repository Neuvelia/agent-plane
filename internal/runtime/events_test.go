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

func TestRunEmitsCorrelatedLifecycleAndUniqueIDs(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	first, err := Run(context.Background(), def, nil, exec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), def, nil, exec, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == "" || first.RunID == second.RunID || first.Nodes[0].NodeRunID == "" || first.Nodes[0].NodeRunID == second.Nodes[0].NodeRunID {
		t.Fatalf("IDs: %#v %#v", first, second)
	}
	want := []events.Type{events.RunStarted, events.NodeReady, events.NodeStarted, events.NodeFinished, events.RunFinished}
	if len(first.Events) != len(want) {
		t.Fatalf("events = %#v", first.Events)
	}
	for i, event := range first.Events {
		if event.Type != want[i] || event.Sequence != uint64(i+1) || event.RunID != first.RunID || event.GraphID != def.ID || event.GraphVersion != first.GraphVersion || event.Timestamp.IsZero() {
			t.Fatalf("event %d = %#v", i, event)
		}
		if i > 0 && i < 4 && (event.NodeID != "worker" || event.NodeRunID != first.Nodes[0].NodeRunID) {
			t.Fatalf("node correlation = %#v", event)
		}
		if _, err := json.Marshal(event); err != nil {
			t.Fatalf("event %d JSON: %v", i, err)
		}
	}
	if first.GraphVersion == "" || first.Events[3].Payload.(events.NodeFinishedPayload).Status != string(NodeSucceeded) || first.Events[4].Payload.(events.RunFinishedPayload).Status != string(RunSucceeded) {
		t.Fatalf("terminal payloads = %#v", first.Events)
	}
}

func TestUnstartedTerminalsNeverEmitNodeFinished(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "test"}, "b": {Type: "test"}})
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "a" {
			return nil, errors.New("private error")
		}
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{MaxConcurrency: 1})
	if err != nil || result.State != RunFailed {
		t.Fatalf("result = %#v, %v", result, err)
	}
	var got []events.Type
	for _, event := range result.Events {
		got = append(got, event.Type)
	}
	want := []events.Type{events.RunStarted, events.NodeReady, events.NodeReady, events.NodeStarted, events.NodeFinished, events.NodeTerminated, events.RunFinished}
	if len(got) != len(want) {
		t.Fatalf("types = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("types = %v, want %v", got, want)
		}
	}
	if result.Events[len(result.Events)-1].Payload.(events.RunFinishedPayload).Status != string(RunFailed) {
		t.Fatalf("terminal = %#v", result.Events[len(result.Events)-1])
	}
	data, _ := json.Marshal(result.Events)
	if strings.Contains(string(data), "private error") {
		t.Fatalf("raw error leaked: %s", data)
	}
}

func TestConcurrentRunEventsHaveUniqueMonotonicSequence(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "test"}, "b": {Type: "test"}})
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		started <- struct{}{}
		<-release
		return nil, nil
	})
	var result RunResult
	var err error
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		result, err = Run(context.Background(), def, nil, exec, Options{MaxConcurrency: 2})
	}()
	<-started
	<-started
	close(release)
	group.Wait()
	if err != nil || result.State != RunSucceeded {
		t.Fatalf("result = %#v, %v", result, err)
	}
	if result.Nodes[0].NodeRunID == result.Nodes[1].NodeRunID {
		t.Fatalf("node run IDs collide: %#v", result.Nodes)
	}
	for i, event := range result.Events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d = %d", i, event.Sequence)
		}
	}
	if result.Events[len(result.Events)-1].Type != events.RunFinished {
		t.Fatalf("last event = %#v", result.Events[len(result.Events)-1])
	}
}

func TestBindingFailureEmitsTerminalWithoutStart(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{
		"source": {Type: "test", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		"target": {Type: "test", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
	}, graph.EdgeDefinition{From: "source.outputs.value", To: "target.inputs.value"})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{})
	if err != nil || result.State != RunFailed {
		t.Fatalf("result = %#v, %v", result, err)
	}
	var targetStarted, targetTerminal bool
	for _, event := range result.Events {
		if event.NodeID != "target" {
			continue
		}
		if event.Type == events.NodeStarted {
			targetStarted = true
		}
		if event.Type == events.NodeTerminated {
			payload := event.Payload.(events.NodeTerminatedPayload)
			targetTerminal = payload.Status == string(NodeFailed) && payload.FailureKind == string(FailureBinding) && len(payload.DiagnosticCodes) == 1 && payload.DiagnosticCodes[0] == "E_BOUND_OUTPUT_VALUE_MISSING"
		}
	}
	if targetStarted || !targetTerminal || result.Events[len(result.Events)-1].Type != events.RunFinished {
		t.Fatalf("events = %#v", result.Events)
	}
}

func TestCancelledRunHasOneFinalEvent(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "test"}, "b": {Type: "test"}})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	exec := executorFunc(func(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan RunResult, 1)
	go func() { result, _ := Run(ctx, def, nil, exec, Options{MaxConcurrency: 1}); done <- result }()
	<-started
	cancel()
	result := <-done
	if result.State != RunCancelled {
		t.Fatalf("state = %s", result.State)
	}
	var terminalCount int
	for i, event := range result.Events {
		if event.Type == events.RunFinished {
			terminalCount++
			if i != len(result.Events)-1 || event.Payload.(events.RunFinishedPayload).Status != string(RunCancelled) {
				t.Fatalf("terminal event = %#v", event)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal count = %d", terminalCount)
	}
}

func TestPreflightFailureHasNoRunIdentityOrEvents(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	def.ID = "INVALID"
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		t.Fatal("executor called")
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{})
	if err == nil || result.RunID != "" || len(result.Events) != 0 {
		t.Fatalf("preflight = %#v, %v", result, err)
	}
}

func TestGraphOutputFailureFinishesAfterSuccessfulNode(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}}})
	def.Outputs["answer"] = graph.OutputDefinition{From: "worker.outputs.value"}
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{})
	if err != nil || result.State != RunFailed {
		t.Fatalf("result = %#v, %v", result, err)
	}
	last := result.Events[len(result.Events)-1]
	if last.Type != events.RunFinished || last.Payload.(events.RunFinishedPayload).FailureKind != string(FailureGraphOutput) || result.Events[len(result.Events)-2].Payload.(events.NodeFinishedPayload).Status != string(NodeSucceeded) {
		t.Fatalf("events = %#v", result.Events)
	}
}
