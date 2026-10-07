package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"agentplane/internal/graph"
)

// Executor runs one node with resolved inputs. Implementations must respect
// ctx and treat the supplied node definition as read-only. If execution ends
// because of cancellation, the returned error must wrap context.Canceled or
// context.DeadlineExceeded so the scheduler can distinguish it from a failure.
type Executor interface {
	ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error)
}

// Options controls the maximum number of simultaneously running nodes.
// Zero selects the conservative default of one. Negative values are invalid.
type Options struct {
	MaxConcurrency int
}

type completedNode struct {
	id      string
	outputs map[string]graph.Value
	err     error
}

type runMachine struct {
	ids      []string
	nodes    map[string]*NodeResult
	resolved map[string]map[string]graph.Value
	outputs  map[string]map[string]graph.Value
	result   RunResult
	sequence uint64
}

// Run executes one validated graph in memory. A non-nil error means preflight
// failed and no run started. Runtime failures are represented in RunResult.
// Callers must not mutate definition or inputs while Run is active.
func Run(ctx context.Context, definition *graph.Definition, inputs map[string]graph.Value, executor Executor, options Options) (RunResult, error) {
	if ctx == nil {
		return RunResult{}, errors.New("run context is nil")
	}
	if executor == nil {
		return RunResult{}, errors.New("node executor is nil")
	}
	if options.MaxConcurrency < 0 {
		return RunResult{}, fmt.Errorf("max concurrency must not be negative: %d", options.MaxConcurrency)
	}
	limit := options.MaxConcurrency
	if limit == 0 {
		limit = 1
	}

	plan, validation := graph.CompileBindings(definition)
	if !validation.Valid() {
		return RunResult{}, &PreflightError{Diagnostics: validation}
	}
	inputValidation := plan.ValidateGraphInputs(inputs)
	if !inputValidation.Valid() {
		return RunResult{}, &PreflightError{Diagnostics: inputValidation}
	}
	// Run inputs are a stable snapshot even if the caller reuses its map.
	graphInputs := make(map[string]graph.Value, len(inputs))
	for name, value := range inputs {
		graphInputs[name] = value
	}

	machine := newRunMachine(definition)
	if limit > len(machine.ids) {
		limit = len(machine.ids)
	}
	dependencies := make(map[string][]string, len(machine.ids))
	for _, id := range machine.ids {
		dependencies[id], _ = plan.Dependencies(id) // CompileBindings validated every node.
	}

	for {
		if ctx.Err() != nil {
			machine.cancelUnstarted()
			machine.finish(RunCancelled)
			return machine.snapshot(), nil
		}
		failures := machine.promoteReady(ctx, plan, graphInputs, dependencies)
		if len(failures) > 0 {
			machine.result.Failure = failures[0]
			machine.skipUnstarted()
			machine.finish(RunFailed)
			return machine.snapshot(), nil
		}
		if ctx.Err() != nil {
			machine.cancelUnstarted()
			machine.finish(RunCancelled)
			return machine.snapshot(), nil
		}

		batch := machine.nextBatch(limit)
		if len(batch) == 0 {
			if machine.allSucceeded() {
				outputs, resolution := plan.ResolveGraphOutputs(machine.outputs)
				if !resolution.Valid() {
					machine.result.Failure = &Failure{Kind: FailureGraphOutput, Diagnostics: resolution.Diagnostics}
					machine.finish(RunFailed)
				} else {
					machine.result.Outputs = sortedPortValues(outputs)
					machine.finish(RunSucceeded)
				}
			} else {
				machine.result.Failure = &Failure{Kind: FailureScheduler, Err: errors.New("no runnable nodes remain in validated graph")}
				machine.skipUnstarted()
				machine.finish(RunFailed)
			}
			return machine.snapshot(), nil
		}

		// Only workers run concurrently; the coordinator owns every state and
		// output map. Buffering lets all workers return before result collection.
		completed := make(chan completedNode, len(batch))
		started := 0
		for _, id := range batch {
			if ctx.Err() != nil {
				break
			}
			machine.transitionNode(id, NodeRunning)
			node := definition.Nodes[id]
			resolved := machine.resolved[id]
			go func(id string) {
				outputs, err := executor.ExecuteNode(ctx, id, node, resolved)
				completed <- completedNode{id: id, outputs: outputs, err: err}
			}(id)
			started++
		}
		results := make([]completedNode, 0, started)
		for range started {
			results = append(results, <-completed)
		}
		sort.Slice(results, func(i, j int) bool { return results[i].id < results[j].id })
		for _, work := range results {
			nodeResult := machine.nodes[work.id]
			if work.err != nil {
				if ctx.Err() != nil && (errors.Is(work.err, context.Canceled) || errors.Is(work.err, context.DeadlineExceeded)) {
					machine.transitionNode(work.id, NodeCancelled)
				} else {
					nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureExecutor, Err: work.err}
					machine.transitionNode(work.id, NodeFailed)
				}
				continue
			}
			diagnostics := validateNodeOutputs(definition.ID, work.id, definition.Nodes[work.id].Outputs, work.outputs)
			if len(diagnostics) > 0 {
				nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureOutputContract, Diagnostics: diagnostics}
				machine.transitionNode(work.id, NodeFailed)
				continue
			}
			machine.outputs[work.id] = copyValues(work.outputs)
			nodeResult.Outputs = sortedPortValues(work.outputs)
			machine.transitionNode(work.id, NodeSucceeded)
		}
		for _, id := range machine.ids {
			if node := machine.nodes[id]; node.State == NodeFailed {
				machine.result.Failure = node.Failure
				break
			}
		}
		if machine.result.Failure != nil {
			machine.skipUnstarted()
			machine.finish(RunFailed)
			return machine.snapshot(), nil
		}
		if ctx.Err() != nil {
			machine.cancelUnstarted()
			machine.finish(RunCancelled)
			return machine.snapshot(), nil
		}
	}
}

func newRunMachine(definition *graph.Definition) *runMachine {
	ids := make([]string, 0, len(definition.Nodes))
	for id := range definition.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	m := &runMachine{
		ids:      ids,
		nodes:    make(map[string]*NodeResult, len(ids)),
		resolved: make(map[string]map[string]graph.Value, len(ids)),
		outputs:  make(map[string]map[string]graph.Value, len(ids)),
		result:   RunResult{GraphID: definition.ID, State: RunRunning},
	}
	m.record("", "", string(RunRunning))
	for _, id := range ids {
		m.nodes[id] = &NodeResult{NodeID: id, State: NodePending}
	}
	return m
}

func (m *runMachine) record(id, from, to string) {
	m.sequence++
	m.result.Transitions = append(m.result.Transitions, StateTransition{Sequence: m.sequence, NodeID: id, From: from, To: to})
}

func (m *runMachine) transitionNode(id string, to NodeState) {
	from := m.nodes[id].State
	if !validNodeTransition(from, to) {
		panic(fmt.Sprintf("invalid scheduler node transition %q: %s -> %s", id, from, to))
	}
	m.nodes[id].State = to
	m.record(id, string(from), string(to))
}

func (m *runMachine) finish(to RunState) {
	if !validRunTransition(m.result.State, to) {
		panic(fmt.Sprintf("invalid scheduler run transition: %s -> %s", m.result.State, to))
	}
	from := m.result.State
	m.result.State = to
	m.record("", string(from), string(to))
}

func (m *runMachine) promoteReady(ctx context.Context, plan *graph.BindingPlan, graphInputs map[string]graph.Value, dependencies map[string][]string) []*Failure {
	var failures []*Failure
	for _, id := range m.ids {
		if ctx.Err() != nil {
			return failures
		}
		if m.nodes[id].State != NodePending {
			continue
		}
		ready := true
		for _, predecessor := range dependencies[id] {
			if m.nodes[predecessor].State != NodeSucceeded {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		inputs, resolution := plan.ResolveNodeInputs(id, graphInputs, m.outputs)
		if ctx.Err() != nil {
			return failures
		}
		if !resolution.Valid() {
			failure := &Failure{NodeID: id, Kind: FailureBinding, Diagnostics: resolution.Diagnostics}
			m.nodes[id].Failure = failure
			m.transitionNode(id, NodeFailed)
			failures = append(failures, failure)
			continue
		}
		m.resolved[id] = inputs
		m.transitionNode(id, NodeReady)
	}
	return failures
}

func (m *runMachine) nextBatch(limit int) []string {
	batch := make([]string, 0, limit)
	for _, id := range m.ids {
		if m.nodes[id].State == NodeReady {
			batch = append(batch, id)
			if len(batch) == limit {
				break
			}
		}
	}
	return batch
}

func (m *runMachine) allSucceeded() bool {
	for _, id := range m.ids {
		if m.nodes[id].State != NodeSucceeded {
			return false
		}
	}
	return true
}

func (m *runMachine) skipUnstarted() {
	for _, id := range m.ids {
		node := m.nodes[id]
		if node.State == NodePending || node.State == NodeReady {
			node.SkipReason = "run_failed"
			m.transitionNode(id, NodeSkipped)
		}
	}
}

func (m *runMachine) cancelUnstarted() {
	for _, id := range m.ids {
		state := m.nodes[id].State
		if state == NodePending || state == NodeReady {
			m.transitionNode(id, NodeCancelled)
		}
	}
}

func (m *runMachine) snapshot() RunResult {
	result := m.result
	result.Nodes = make([]NodeResult, 0, len(m.ids))
	for _, id := range m.ids {
		result.Nodes = append(result.Nodes, *m.nodes[id])
	}
	return result
}

func sortedPortValues(values map[string]graph.Value) []PortValue {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]PortValue, 0, len(names))
	for _, name := range names {
		result = append(result, PortValue{Name: name, Value: values[name]})
	}
	return result
}

func copyValues(values map[string]graph.Value) map[string]graph.Value {
	copied := make(map[string]graph.Value, len(values))
	for name, value := range values {
		copied[name] = value
	}
	return copied
}

func validateNodeOutputs(graphID, nodeID string, declared map[string]graph.PortDefinition, values map[string]graph.Value) []graph.Diagnostic {
	var diagnostics []graph.Diagnostic
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		port := declared[name]
		value, present := values[name]
		location := graph.DiagnosticLocation{NodeID: nodeID, Path: "nodes." + nodeID + ".outputs." + name}
		if !present && port.Required {
			diagnostics = append(diagnostics, graph.Diagnostic{Severity: graph.SeverityError, Code: "E_NODE_OUTPUT_VALUE_MISSING", GraphID: graphID, Location: location, Message: fmt.Sprintf("required output %q on node %q has no value", name, nodeID), Suggestion: "return the declared output value"})
		} else if present && value.Type() != port.Type {
			diagnostics = append(diagnostics, graph.Diagnostic{Severity: graph.SeverityError, Code: "E_NODE_OUTPUT_VALUE_TYPE_MISMATCH", GraphID: graphID, Location: location, Message: fmt.Sprintf("output %q on node %q expects %s but received %s", name, nodeID, port.Type, value.Type()), Suggestion: "return a value with the declared type"})
		}
	}
	names = names[:0]
	for name := range values {
		if _, ok := declared[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		diagnostics = append(diagnostics, graph.Diagnostic{Severity: graph.SeverityError, Code: "E_NODE_OUTPUT_UNDECLARED", GraphID: graphID, Location: graph.DiagnosticLocation{NodeID: nodeID, Path: fmt.Sprintf("nodes.%s.outputs[%q]", nodeID, name)}, Message: fmt.Sprintf("node %q returned undeclared output %q", nodeID, name), Suggestion: "declare the output or stop returning it"})
	}
	return diagnostics
}
