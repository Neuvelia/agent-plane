package runtime

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

type streamingExecutorFunc func(context.Context, string, graph.NodeDefinition, map[string]graph.Value, func(string, []byte)) (map[string]graph.Value, error)

func (f streamingExecutorFunc) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	return nil, errors.New("streaming execution was not used")
}

func (f streamingExecutorFunc) ExecuteNodeWithLogs(ctx context.Context, id string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
	return f(ctx, id, node, inputs, observe)
}

func TestRunRedactsSplitSecretsBeforeLogEvents(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	exec := streamingExecutorFunc(func(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
		observe("stdout", []byte("before sec"))
		observe("stderr", []byte("error sec"))
		observe("stdout", []byte("ret after"))
		observe("stderr", []byte("ret done"))
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, LogRedactions: []string{"secret"}})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	var stdout, stderr bytes.Buffer
	for i, event := range result.Events {
		if event.Type != events.LogChunk {
			continue
		}
		if event.NodeID != "worker" || event.NodeRunID != result.Nodes[0].NodeRunID || i == 0 || i >= len(result.Events)-2 {
			t.Fatalf("log correlation/order = %#v", event)
		}
		payload := event.Payload.(events.LogChunkPayload)
		switch payload.Stream {
		case events.Stdout:
			stdout.Write(payload.Data)
		case events.Stderr:
			stderr.Write(payload.Data)
		default:
			t.Fatalf("stream = %q", payload.Stream)
		}
	}
	if stdout.String() != "before *** after" || stderr.String() != "error *** done" {
		t.Fatalf("logs stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsRedactionsWithoutSafeMarkerBeforeStart(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		t.Fatal("executor called")
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, LogRedactions: []string{string(allBytes)}})
	if err == nil || result.RunID != "" || len(result.Events) != 0 {
		t.Fatalf("invalid redactions started a run: %#v, %v", result, err)
	}
}

func TestConcurrentRunLogCorrelation(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "test"}, "b": {Type: "test"}})
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	exec := streamingExecutorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
		started <- struct{}{}
		<-release
		observe("stdout", []byte(id+"-out"))
		observe("stderr", []byte(id+"-err"))
		return nil, nil
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 2})
		done <- result
	}()
	<-started
	<-started
	close(release)
	result := <-done
	if result.State != run.RunSucceeded {
		t.Fatalf("run = %#v", result)
	}
	ids := map[string]events.NodeRunID{}
	for _, node := range result.Nodes {
		ids[node.NodeID] = node.NodeRunID
	}
	logs := map[string]map[events.Stream]string{}
	for i, event := range result.Events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d = %d", i, event.Sequence)
		}
		if event.Type != events.LogChunk {
			continue
		}
		if event.NodeRunID != ids[event.NodeID] {
			t.Fatalf("wrong node run ID: %#v", event)
		}
		payload := event.Payload.(events.LogChunkPayload)
		if logs[event.NodeID] == nil {
			logs[event.NodeID] = map[events.Stream]string{}
		}
		logs[event.NodeID][payload.Stream] += string(payload.Data)
	}
	for _, id := range []string{"a", "b"} {
		if logs[id][events.Stdout] != id+"-out" || logs[id][events.Stderr] != id+"-err" {
			t.Fatalf("%s logs = %#v", id, logs[id])
		}
	}
}

func TestLateLogCallbackDoesNotChangeFinishedRun(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	var late func(string, []byte)
	exec := streamingExecutorFunc(func(_ context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
		observe("stdout", []byte("during"))
		late = observe
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("run = %#v, %v", result, err)
	}
	var callbacks sync.WaitGroup
	for _, stream := range []string{"stdout", "stderr"} {
		callbacks.Add(1)
		go func() {
			defer callbacks.Done()
			late(stream, []byte("after"))
		}()
	}
	callbacks.Wait()
	if got := len(result.Events); got != 6 {
		t.Fatalf("events = %d, want 6", got)
	}
	if result.Events[4].Type != events.NodeFinished || result.Events[5].Type != events.RunFinished {
		t.Fatalf("terminal events = %#v", result.Events[4:])
	}
}

func TestRunEmitsCorrelatedLifecycleAndUniqueIDs(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	first, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal})
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
	if first.GraphVersion == "" || first.Events[3].Payload.(events.NodeFinishedPayload).Status != string(run.NodeSucceeded) || first.Events[4].Payload.(events.RunFinishedPayload).Status != string(run.RunSucceeded) {
		t.Fatalf("terminal payloads = %#v", first.Events)
	}
}

func TestRunDeliversLiveEventsInSequence(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "test"}})
	started := make(chan struct{})
	release := make(chan struct{})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		close(started)
		<-release
		return nil, nil
	})
	live := make(chan events.Event)
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, LiveEvents: live})
		done <- result
	}()
	var received []events.Event
	go func() { result := <-done; close(live); done <- result }()
	for event := range live {
		received = append(received, event)
		if event.Type == events.NodeStarted {
			<-started
			close(release)
		}
	}
	result := <-done
	for i, e := range received {
		if e.Sequence != result.Events[i].Sequence || e.Type != result.Events[i].Type {
			t.Fatal("live prefix mismatch")
		}
	}
	if result.Events[len(result.Events)-1].Type != events.RunFinished {
		t.Fatal("terminal fact missing")
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 1})
	if err != nil || result.State != run.RunFailed {
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
	if result.Events[len(result.Events)-1].Payload.(events.RunFinishedPayload).Status != string(run.RunFailed) {
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
	var result run.Result
	var err error
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		result, err = Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 2})
	}()
	<-started
	<-started
	close(release)
	group.Wait()
	if err != nil || result.State != run.RunSucceeded {
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != run.RunFailed {
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
			targetTerminal = payload.Status == string(run.NodeFailed) && payload.FailureKind == string(run.FailureBinding) && len(payload.DiagnosticCodes) == 1 && payload.DiagnosticCodes[0] == "E_BOUND_OUTPUT_VALUE_MISSING"
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
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(ctx, def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 1})
		done <- result
	}()
	<-started
	cancel()
	result := <-done
	if result.State != run.RunCancelled {
		t.Fatalf("state = %s", result.State)
	}
	var terminalCount int
	for i, event := range result.Events {
		if event.Type == events.RunFinished {
			terminalCount++
			if i != len(result.Events)-1 || event.Payload.(events.RunFinishedPayload).Status != string(run.RunCancelled) {
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal})
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
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != run.RunFailed {
		t.Fatalf("result = %#v, %v", result, err)
	}
	last := result.Events[len(result.Events)-1]
	if last.Type != events.RunFinished || last.Payload.(events.RunFinishedPayload).FailureKind != string(run.FailureGraphOutput) || result.Events[len(result.Events)-2].Payload.(events.NodeFinishedPayload).Status != string(run.NodeSucceeded) {
		t.Fatalf("events = %#v", result.Events)
	}
}
