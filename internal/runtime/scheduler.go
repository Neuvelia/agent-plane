package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

// Executor runs one node with resolved inputs. Implementations must respect
// ctx and treat the supplied node definition as read-only. If execution ends
// because of cancellation, the returned error must wrap context.Canceled or
// context.DeadlineExceeded so the scheduler can distinguish it from a failure.
type Executor interface {
	ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error)
}

// LogStreamingExecutor optionally reports bounded output chunks while a node
// executes. The observer may be called concurrently for separate streams; all
// callbacks must complete before ExecuteNodeWithLogs returns.
type LogStreamingExecutor interface {
	ExecuteNodeWithLogs(context.Context, string, graph.NodeDefinition, map[string]graph.Value, func(string, []byte)) (map[string]graph.Value, error)
}

// Options controls the maximum number of simultaneously running nodes.
// Zero selects the conservative default of one. Negative values are invalid.
type Options struct {
	MaxConcurrency int
	// LogSanitizer runs before output bytes enter the event collector. It must
	// be safe for concurrent calls from stdout and stderr writers.
	LogSanitizer func([]byte) []byte
}

type completedNode struct {
	id      string
	outputs map[string]graph.Value
	err     error
}

const maxEventLogChunk = 16 * 1024

type runMachine struct {
	ids         []string
	nodes       map[string]*NodeResult
	resolved    map[string]map[string]graph.Value
	outputs     map[string]map[string]graph.Value
	result      RunResult
	emitter     *events.Collector
	startedAt   time.Time
	nodeStarted map[string]time.Time
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

	machine, err := newRunMachine(definition)
	if err != nil {
		return RunResult{}, err
	}
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
				var outputs map[string]graph.Value
				var err error
				if streaming, ok := executor.(LogStreamingExecutor); ok {
					observer := func(stream string, data []byte) {
						if stream != string(events.Stdout) && stream != string(events.Stderr) {
							return
						}
						if options.LogSanitizer != nil {
							data = options.LogSanitizer(data)
						}
						for len(data) > 0 {
							chunk := data
							if len(chunk) > maxEventLogChunk {
								chunk = chunk[:maxEventLogChunk]
							}
							machine.emitter.Append(events.Event{Type: events.LogChunk, NodeID: id, NodeRunID: machine.nodes[id].NodeRunID, Payload: events.LogChunkPayload{Stream: events.Stream(stream), Data: chunk}})
							data = data[len(chunk):]
						}
					}
					outputs, err = streaming.ExecuteNodeWithLogs(ctx, id, node, resolved, observer)
				} else {
					outputs, err = executor.ExecuteNode(ctx, id, node, resolved)
				}
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

func newRunMachine(definition *graph.Definition) (*runMachine, error) {
	version, err := graph.SemanticVersion(definition)
	if err != nil {
		return nil, err
	}
	runID, err := events.NewRunID()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(definition.Nodes))
	for id := range definition.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	m := &runMachine{
		ids:         ids,
		nodes:       make(map[string]*NodeResult, len(ids)),
		resolved:    make(map[string]map[string]graph.Value, len(ids)),
		outputs:     make(map[string]map[string]graph.Value, len(ids)),
		result:      RunResult{GraphID: definition.ID, GraphVersion: version, RunID: runID, State: RunRunning},
		emitter:     events.NewCollector(runID, definition.ID, version),
		nodeStarted: make(map[string]time.Time, len(ids)),
	}
	for _, id := range ids {
		nodeRunID, err := events.NewNodeRunID()
		if err != nil {
			return nil, err
		}
		m.nodes[id] = &NodeResult{NodeID: id, NodeRunID: nodeRunID, State: NodePending}
	}
	m.record("", "", string(RunRunning))
	return m, nil
}

func (m *runMachine) record(id, from, to string) {
	event := events.Event{NodeID: id}
	if id == "" {
		if from == "" {
			event.Type = events.RunStarted
			event.Payload = events.RunStartedPayload{NodeCount: len(m.ids)}
		} else {
			event.Type = events.RunFinished
			counts := events.NodeCounts{}
			for _, nodeID := range m.ids {
				switch m.nodes[nodeID].State {
				case NodeSucceeded:
					counts.Succeeded++
				case NodeFailed:
					counts.Failed++
				case NodeSkipped:
					counts.Skipped++
				case NodeCancelled:
					counts.Cancelled++
				}
			}
			failureKind, _ := eventFailure(m.result.Failure)
			event.Payload = events.RunFinishedPayload{Status: to, DurationMS: time.Since(m.startedAt).Milliseconds(), NodeCounts: counts, FailureKind: failureKind}
		}
	} else {
		node := m.nodes[id]
		event.NodeRunID = node.NodeRunID
		switch to {
		case string(NodeReady):
			event.Type, event.Payload = events.NodeReady, events.NodeReadyPayload{}
		case string(NodeRunning):
			event.Type, event.Payload = events.NodeStarted, events.NodeStartedPayload{}
		default:
			failureKind, codes := eventFailure(node.Failure)
			if from == string(NodeRunning) {
				duration := time.Since(m.nodeStarted[id]).Milliseconds()
				event.Type, event.Payload = events.NodeFinished, events.NodeFinishedPayload{Status: to, FailureKind: failureKind, DiagnosticCodes: codes, DurationMS: &duration}
			} else {
				event.Type, event.Payload = events.NodeTerminated, events.NodeTerminatedPayload{Status: to, FailureKind: failureKind, DiagnosticCodes: codes}
			}
		}
	}
	recorded := m.emitter.Append(event)
	if id == "" && from == "" {
		m.startedAt = recorded.Timestamp
	}
	if id != "" && to == string(NodeRunning) {
		m.nodeStarted[id] = recorded.Timestamp
	}
	m.result.Transitions = append(m.result.Transitions, StateTransition{Sequence: recorded.Sequence, NodeID: id, From: from, To: to})
}

func eventFailure(failure *Failure) (string, []string) {
	if failure == nil {
		return "", nil
	}
	codes := make([]string, 0, len(failure.Diagnostics))
	for _, diagnostic := range failure.Diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	sort.Strings(codes)
	return string(failure.Kind), codes
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
	result.Events = m.emitter.Snapshot()
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
