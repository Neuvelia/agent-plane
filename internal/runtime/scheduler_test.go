package runtime

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

type executorFunc func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error)

type cancelAfterFirstErr struct {
	context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelAfterFirstErr) Err() error {
	err := c.Context.Err()
	c.once.Do(c.cancel)
	return err
}

func (f executorFunc) ExecuteNode(ctx context.Context, id string, node graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
	return f(ctx, id, node, inputs)
}

func definition(nodes map[string]graph.NodeDefinition, edges ...graph.EdgeDefinition) *graph.Definition {
	return &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "scheduler-test", Inputs: map[string]graph.InputDefinition{}, Nodes: nodes, Edges: append([]graph.EdgeDefinition{}, edges...), Outputs: map[string]graph.OutputDefinition{}}
}

func nodeResult(t *testing.T, result run.Result, id string) run.NodeResult {
	t.Helper()
	for _, node := range result.Nodes {
		if node.NodeID == id {
			return node
		}
	}
	t.Fatalf("node %q absent from result %#v", id, result.Nodes)
	return run.NodeResult{}
}

func peakRunningTransitions(t *testing.T, transitions []run.StateTransition) int {
	t.Helper()
	running, peak := 0, 0
	for _, transition := range transitions {
		if transition.NodeID == "" {
			continue
		}
		if transition.To == string(run.NodeRunning) {
			running++
			if running > peak {
				peak = running
			}
		}
		if transition.From == string(run.NodeRunning) {
			running--
		}
		if running < 0 {
			t.Fatalf("running count became negative at transition %#v", transition)
		}
	}
	if running != 0 {
		t.Fatalf("%d nodes left running in terminal transition trace", running)
	}
	return peak
}

func recordPeak(peak *atomic.Int32, active int32) {
	for current := peak.Load(); active > current; current = peak.Load() {
		if peak.CompareAndSwap(current, active) {
			return
		}
	}
}

func TestRunRejectsOptionsBeforeGraphValidationWithoutStarting(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "shell"}})
	def.ID = "INVALID"
	var executed, persisted atomic.Bool
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		executed.Store(true)
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: -1, Persist: func(events.Event) error {
		persisted.Store(true)
		return nil
	}})
	if err == nil || err.Error() != "max concurrency must not be negative: -1" {
		t.Fatalf("preflight error = %v", err)
	}
	if result.RunID != "" || len(result.Events) != 0 || executed.Load() || persisted.Load() {
		t.Fatalf("invalid options started a run: result=%#v executed=%t persisted=%t", result, executed.Load(), persisted.Load())
	}
}

func TestRunLinearDAGResolvesBoundInputs(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{
		"a": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		"b": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		"c": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
	},
		graph.EdgeDefinition{From: "a.outputs.value", To: "b.inputs.value"},
		graph.EdgeDefinition{From: "b.outputs.value", To: "c.inputs.value"},
	)
	def.Outputs["answer"] = graph.OutputDefinition{From: "c.outputs.value"}
	var calls []string
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
		calls = append(calls, id)
		if id != "a" {
			value, ok := inputs["value"].AsString()
			if !ok || value != map[string]string{"b": "a", "c": "ab"}[id] {
				t.Errorf("%s inputs = %#v", id, inputs)
			}
		}
		return map[string]graph.Value{"value": graph.NewStringValue(map[string]string{"a": "a", "b": "ab", "c": "abc"}[id])}, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 3})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("Run() = (%#v, %v)", result, err)
	}
	if !reflect.DeepEqual(calls, []string{"a", "b", "c"}) || !reflect.DeepEqual(result.Outputs, []run.PortValue{{Name: "answer", Value: graph.NewStringValue("abc")}}) {
		t.Fatalf("calls/outputs = %v / %#v", calls, result.Outputs)
	}
	for _, node := range result.Nodes {
		if node.State != run.NodeSucceeded {
			t.Fatalf("node %s = %s", node.NodeID, node.State)
		}
	}
}

// roadmap: F0.3-AC2, F0.4-AC1, F0.4-AC2
func TestRunDiamondWaitsForBothBranchesAndAllowsOverlap(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{
		"left":  {Type: "shell", Outputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString}}},
		"right": {Type: "shell", Outputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString}}},
		"merge": {Type: "shell", Inputs: map[string]graph.PortDefinition{
			"left": {Type: graph.ValueTypeString, Required: true}, "right": {Type: graph.ValueTypeString, Required: true},
		}},
	},
		graph.EdgeDefinition{From: "left.outputs.text", To: "merge.inputs.left"},
		graph.EdgeDefinition{From: "right.outputs.text", To: "merge.inputs.right"},
	)
	started := make(chan string, 3)
	releaseLeft, releaseRight := make(chan struct{}), make(chan struct{})
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
		started <- id
		switch id {
		case "left":
			<-releaseLeft
			return map[string]graph.Value{"text": graph.NewStringValue("L")}, nil
		case "right":
			<-releaseRight
			return map[string]graph.Value{"text": graph.NewStringValue("R")}, nil
		default:
			left, _ := inputs["left"].AsString()
			right, _ := inputs["right"].AsString()
			if left != "L" || right != "R" {
				t.Errorf("merge inputs = %q, %q", left, right)
			}
			return nil, nil
		}
	})
	done := make(chan run.Result, 1)
	go func() {
		result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2})
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
		done <- result
	}()
	first, second := <-started, <-started
	if first == second || (first != "left" && first != "right") || (second != "left" && second != "right") {
		t.Fatalf("concurrent starts = %q, %q", first, second)
	}
	close(releaseLeft)
	select {
	case id := <-started:
		t.Fatalf("%s started before right completed", id)
	default:
	}
	close(releaseRight)
	if id := <-started; id != "merge" {
		t.Fatalf("third start = %s, want merge", id)
	}
	if result := <-done; result.State != run.RunSucceeded {
		t.Fatalf("Run state = %s, failure %#v", result.State, result.Failure)
	}
}

func TestRunConcurrencyLimitOneSerializesDisconnectedNodes(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	started := make(chan string, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		recordPeak(&peak, active.Add(1))
		defer active.Add(-1)
		started <- id
		<-release
		return nil, nil
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1})
		done <- result
	}()
	if id := <-started; id != "a" {
		t.Fatalf("first start = %s", id)
	}
	close(release)
	if id := <-started; id != "b" {
		t.Fatalf("second start = %s", id)
	}
	if result := <-done; result.State != run.RunSucceeded {
		t.Fatalf("state = %s", result.State)
	} else if got := peak.Load(); got != 1 || peakRunningTransitions(t, result.Transitions) != 1 {
		t.Fatalf("peak executor activity = %d, transitions = %#v", got, result.Transitions)
	}
}

func TestRunConcurrencyLimitTwoDoesNotStartThirdNode(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}, "c": {Type: "shell"}})
	started := make(chan string, 3)
	release := make(chan struct{})
	var active, peak atomic.Int32
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		recordPeak(&peak, active.Add(1))
		defer active.Add(-1)
		started <- id
		<-release
		return nil, nil
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2})
		done <- result
	}()
	first, second := <-started, <-started
	if first == second || (first != "a" && first != "b") || (second != "a" && second != "b") {
		t.Fatalf("first batch = %q, %q", first, second)
	}
	close(release)
	if id := <-started; id != "c" {
		t.Fatalf("next start = %s, want c", id)
	}
	if result := <-done; result.State != run.RunSucceeded {
		t.Fatalf("Run state = %s", result.State)
	} else if got := peak.Load(); got != 2 || peakRunningTransitions(t, result.Transitions) != 2 {
		t.Fatalf("peak executor activity = %d, transitions = %#v", got, result.Transitions)
	}
}

func TestRunFailFastSkipsDownstreamAndQueuedIndependentWork(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{
		"a": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		"b": {Type: "shell"},
		"c": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString, Required: true}}},
		"d": {Type: "shell"},
	}, graph.EdgeDefinition{From: "a.outputs.value", To: "c.inputs.value"})
	failure := errors.New("a failed")
	started := make(chan string, 2)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	aReturning := make(chan struct{})
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		started <- id
		if id == "a" {
			<-releaseA
			close(aReturning)
			return nil, failure
		}
		if id == "b" {
			<-releaseB
		}
		return nil, nil
	})
	type runOutcome struct {
		result run.Result
		err    error
	}
	done := make(chan runOutcome, 1)
	go func() {
		result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2})
		done <- runOutcome{result: result, err: err}
	}()
	first, second := <-started, <-started
	if first == second || (first != "a" && first != "b") || (second != "a" && second != "b") {
		t.Fatalf("first batch = %q, %q", first, second)
	}
	close(releaseA)
	<-aReturning
	close(releaseB)
	outcome := <-done
	result, err := outcome.result, outcome.err
	if err != nil || result.State != run.RunFailed || result.Failure == nil || result.Failure.NodeID != "a" || !errors.Is(result.Failure.Err, failure) {
		t.Fatalf("Run() = (%#v, %v)", result, err)
	}
	for id, want := range map[string]run.NodeState{"a": run.NodeFailed, "b": run.NodeSucceeded, "c": run.NodeSkipped, "d": run.NodeSkipped} {
		if got := nodeResult(t, result, id).State; got != want {
			t.Fatalf("node %s = %s, want %s", id, got, want)
		}
	}
}

func TestRunCancellationDoesNotMaskIndependentFailure(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	failure := errors.New("independent failure")
	exec := executorFunc(func(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		close(started)
		<-ctx.Done()
		return nil, failure
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(ctx, def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1})
		done <- result
	}()
	<-started
	cancel()
	result := <-done
	if result.State != run.RunFailed || result.Failure == nil || !errors.Is(result.Failure.Err, failure) ||
		nodeResult(t, result, "a").State != run.NodeFailed || nodeResult(t, result, "b").State != run.NodeSkipped {
		t.Fatalf("failure after cancellation = %#v", result)
	}
}

func TestRunChoosesLowestNodeIDWhenConcurrentNodesFail(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	started := make(chan string, 2)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		started <- id
		if id == "a" {
			<-releaseA
		} else {
			<-releaseB
		}
		return nil, errors.New(id + " failed")
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2})
		done <- result
	}()
	<-started
	<-started
	close(releaseB)
	close(releaseA)
	result := <-done
	if result.State != run.RunFailed || result.Failure == nil || result.Failure.NodeID != "a" || nodeResult(t, result, "b").State != run.NodeFailed {
		t.Fatalf("result = %#v", result)
	}
}

// roadmap: F0.3-AC1
func TestRunMissingBoundOutputFailsConsumerBeforeExecution(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{
		"source": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		"target": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
	}, graph.EdgeDefinition{From: "source.outputs.value", To: "target.inputs.value"})
	calledTarget := false
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		if id == "target" {
			calledTarget = true
		}
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunFailed || calledTarget || nodeResult(t, result, "source").State != run.NodeSucceeded {
		t.Fatalf("Run() = (%#v, %v), target called=%v", result, err, calledTarget)
	}
	failure := nodeResult(t, result, "target").Failure
	if failure == nil || failure.Kind != run.FailureBinding || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != "E_BOUND_OUTPUT_VALUE_MISSING" {
		t.Fatalf("target failure = %#v", failure)
	}
}

func TestRunRejectsInvalidExecutorOutputs(t *testing.T) {
	for _, test := range []struct {
		name    string
		port    graph.PortDefinition
		outputs map[string]graph.Value
		code    string
	}{
		{"missing required", graph.PortDefinition{Type: graph.ValueTypeString, Required: true}, nil, "E_NODE_OUTPUT_VALUE_MISSING"},
		{"wrong type", graph.PortDefinition{Type: graph.ValueTypeString}, map[string]graph.Value{"value": graph.NewBooleanValue(true)}, "E_NODE_OUTPUT_VALUE_TYPE_MISMATCH"},
		{"undeclared", graph.PortDefinition{Type: graph.ValueTypeString}, map[string]graph.Value{"extra": graph.NewStringValue("x")}, "E_NODE_OUTPUT_UNDECLARED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			def := definition(map[string]graph.NodeDefinition{"worker": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": test.port}}})
			exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
				return test.outputs, nil
			})
			result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
			if err != nil || result.State != run.RunFailed || nodeResult(t, result, "worker").State != run.NodeFailed {
				t.Fatalf("Run() = (%#v, %v)", result, err)
			}
			failure := result.Failure
			if failure == nil || failure.Kind != run.FailureOutputContract || len(failure.Diagnostics) != 1 || failure.Diagnostics[0].Code != test.code {
				t.Fatalf("failure = %#v", failure)
			}
		})
	}
}

func TestRunValidatesGraphInputsAndResolvesBeforeExecution(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}}})
	def.Inputs["message"] = graph.InputDefinition{Type: graph.ValueTypeString, Required: true}
	def.GraphInputBindings = []graph.GraphInputBindingDefinition{{From: "inputs.message", To: "worker.inputs.text"}}
	called := false
	exec := executorFunc(func(_ context.Context, _ string, _ graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
		called = true
		if got, ok := inputs["text"].AsString(); !ok || got != "hello" {
			t.Errorf("input = (%q, %v)", got, ok)
		}
		return nil, nil
	})
	if _, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}}); err == nil || called {
		t.Fatalf("missing required graph input: err=%v called=%v", err, called)
	}
	result, err := Run(context.Background(), def, map[string]graph.Value{"message": graph.NewStringValue("hello")}, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunSucceeded || !called {
		t.Fatalf("Run() = (%#v, %v), called=%v", result, err, called)
	}
}

func TestRunCancellationStopsDispatchAndTerminates(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	exec := executorFunc(func(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(ctx, def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1})
		done <- result
	}()
	<-started
	cancel()
	result := <-done
	if result.State != run.RunCancelled || nodeResult(t, result, "a").State != run.NodeCancelled || nodeResult(t, result, "b").State != run.NodeCancelled {
		t.Fatalf("cancel result = %#v", result)
	}
}

func TestRunCancellationAcceptsChildContextError(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	exec := executorFunc(func(ctx context.Context, _ string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		close(started)
		<-ctx.Done()
		return nil, fmt.Errorf("child stopped: %w", context.DeadlineExceeded)
	})
	done := make(chan run.Result, 1)
	go func() {
		result, _ := Run(ctx, def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1})
		done <- result
	}()
	<-started
	cancel()
	result := <-done
	if result.State != run.RunCancelled || nodeResult(t, result, "a").State != run.NodeCancelled || nodeResult(t, result, "b").State != run.NodeCancelled {
		t.Fatalf("cancel result = %#v", result)
	}
}

func TestRunDoesNotDispatchAfterCancellationDuringReadiness(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}, "b": {Type: "shell"}})
	base, cancel := context.WithCancel(context.Background())
	ctx := &cancelAfterFirstErr{Context: base, cancel: cancel}
	called := false
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		called = true
		return nil, nil
	})
	result, err := Run(ctx, def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2})
	if err != nil || called || result.State != run.RunCancelled || nodeResult(t, result, "a").State != run.NodeCancelled || nodeResult(t, result, "b").State != run.NodeCancelled {
		t.Fatalf("Run() = (%#v, %v), executor called=%v", result, err, called)
	}
}

// roadmap: F0.4-AC3
func TestRunTransitionsAreOrderedAndTerminalExactlyOnce(t *testing.T) {
	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "shell"}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("Run() = (%#v, %v)", result, err)
	}
	want := []run.StateTransition{
		{Sequence: 1, From: "", To: "running"},
		{Sequence: 2, NodeID: "worker", From: "pending", To: "ready"},
		{Sequence: 3, NodeID: "worker", From: "ready", To: "running"},
		{Sequence: 4, NodeID: "worker", From: "running", To: "succeeded"},
		{Sequence: 5, From: "running", To: "succeeded"},
	}
	if !reflect.DeepEqual(result.Transitions, want) {
		t.Fatalf("transitions = %#v, want %#v", result.Transitions, want)
	}
}

func TestRunEmptyGraphAndGraphOutputFailure(t *testing.T) {
	empty := definition(map[string]graph.NodeDefinition{})
	called := false
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		called = true
		return nil, nil
	})
	result, err := Run(context.Background(), empty, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunSucceeded || called || len(result.Nodes) != 0 {
		t.Fatalf("empty run = (%#v, %v), called=%v", result, err, called)
	}

	def := definition(map[string]graph.NodeDefinition{"worker": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}}})
	def.Outputs["answer"] = graph.OutputDefinition{From: "worker.outputs.value"}
	result, err = Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if err != nil || result.State != run.RunFailed || result.Failure == nil || result.Failure.Kind != run.FailureGraphOutput || nodeResult(t, result, "worker").State != run.NodeSucceeded {
		t.Fatalf("missing exposed output = (%#v, %v)", result, err)
	}
}

func TestRunResultsAndTransitionsAreStable(t *testing.T) {
	stable := func(result run.Result) run.Result {
		result.RunID = ""
		result.Events = nil // Timestamps and correlation IDs are unique per run.
		result.Nodes = append([]run.NodeResult(nil), result.Nodes...)
		for i := range result.Nodes {
			result.Nodes[i].NodeRunID = ""
		}
		return result
	}
	def := definition(map[string]graph.NodeDefinition{
		"zeta":  {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		"alpha": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
	})
	def.Outputs["last"] = graph.OutputDefinition{From: "zeta.outputs.value"}
	def.Outputs["first"] = graph.OutputDefinition{From: "alpha.outputs.value"}
	exec := executorFunc(func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value) (map[string]graph.Value, error) {
		return map[string]graph.Value{"value": graph.NewStringValue(id)}, nil
	})
	var first run.Result
	for i := range 20 {
		result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 2})
		if err != nil || result.State != run.RunSucceeded {
			t.Fatalf("run %d = (%#v, %v)", i, result, err)
		}
		if i == 0 {
			first = result
		} else if !reflect.DeepEqual(stable(first), stable(result)) {
			t.Fatalf("run %d differs: first=%#v next=%#v", i, first, result)
		}
	}
	if !reflect.DeepEqual([]string{first.Nodes[0].NodeID, first.Nodes[1].NodeID}, []string{"alpha", "zeta"}) ||
		!reflect.DeepEqual([]string{first.Outputs[0].Name, first.Outputs[1].Name}, []string{"first", "last"}) {
		t.Fatalf("ordered results = %#v / %#v", first.Nodes, first.Outputs)
	}
}

func TestRunStartedRecordsOptionalRunMetadata(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	def := definition(map[string]graph.NodeDefinition{"a": {Type: "shell"}})
	exec := executorFunc(func(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
		return nil, nil
	})
	for name, provider := range map[string]func(context.Context) RunMetadata{
		"recorded": func(context.Context) RunMetadata {
			return RunMetadata{RepoName: "r", RepoBranch: "main", RepoCommit: "abc123", RuntimeVersion: "9.9"}
		},
		"panicking": func(context.Context) RunMetadata { panic("boom") },
		"absent":    nil,
		// A provider that ignores its context cannot hold run_started past the
		// bound (F2.9, F3.6).
		"ignoring context": func(context.Context) RunMetadata { <-release; return RunMetadata{RepoName: "late"} },
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Run(context.Background(), def, nil, exec, Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1, RunMetadata: provider})
			if err != nil || result.State != run.RunSucceeded {
				t.Fatalf("Run() = (%#v, %v)", result.State, err)
			}
			var got events.RunStartedPayload
			for _, e := range result.Events {
				if p, ok := e.Payload.(events.RunStartedPayload); ok {
					got = p
				}
			}
			want := events.RunStartedPayload{NodeCount: 1}
			if name == "recorded" {
				want = events.RunStartedPayload{NodeCount: 1, RepoName: "r", RepoBranch: "main", RepoCommit: "abc123", RuntimeVersion: "9.9"}
			}
			if got != want {
				t.Fatalf("run_started = %#v, want %#v", got, want)
			}
		})
	}
}
