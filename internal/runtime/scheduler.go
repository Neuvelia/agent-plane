package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"agentplane/internal/events"
	"agentplane/internal/gitstate"
	"agentplane/internal/graph"
)

// Executor runs one node with resolved inputs. Implementations must respect
// ctx and treat the supplied node definition as read-only. If execution ends
// because of cancellation, the returned error must wrap context.Canceled or
// context.DeadlineExceeded so the scheduler can distinguish it from a failure.
type Executor interface {
	ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error)
}

// ExecutionCorrelation is created by the scheduler and passed to executors
// that need to associate external work with the run's lifecycle facts.
type ExecutionCorrelation struct {
	RunID     events.RunID
	NodeRunID events.NodeRunID
	AttemptID events.AttemptID
}

// CorrelatedExecutor is optional; legacy executors keep the F0.4 API.
type CorrelatedExecutor interface {
	ExecuteNodeWithCorrelation(context.Context, string, graph.NodeDefinition, map[string]graph.Value, ExecutionCorrelation) (map[string]graph.Value, error)
}

// LogStreamingExecutor optionally reports bounded output chunks while a node
// executes. The observer may be called concurrently for separate streams; all
// callbacks must complete before ExecuteNodeWithLogs returns for complete logs.
// Calls after return are discarded once the worker closes the observer.
type LogStreamingExecutor interface {
	ExecuteNodeWithLogs(context.Context, string, graph.NodeDefinition, map[string]graph.Value, func(string, []byte)) (map[string]graph.Value, error)
}

// ExecutionObservation combines correlation and safe logs without provider types.
// Agent evidence is returned separately and committed only on success. Check
// observes at most one validated Gate fact, retained for green and red. Capture
// capabilities redact before truncation without exposing configured secrets.
type ExecutionObservation struct {
	// AttemptNumber is the 1-based produce attempt for loop producers, else 0.
	AttemptNumber      int
	ScopeBaseline      *gitstate.Baseline
	ScopeBaselineError error
	Feedback           *CorrectionFeedback
	Correlation        ExecutionCorrelation
	Log                func(string, []byte)
	Progress           func(events.Type, events.Payload)
	Check              func(events.GateCheckPayload)
	NewCheckCapture    func() *CheckCapture
}
type ObservedExecutor interface {
	ExecuteNodeObserved(context.Context, string, graph.NodeDefinition, map[string]graph.Value, ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error)
}

// Options controls the maximum number of simultaneously running nodes.
// Zero selects the conservative default of one. Negative values are invalid.
type Options struct {
	MaxConcurrency int
	// LiveEvents receives sequenced events during execution. The caller must
	// consume while Run is active and must not close the channel until Run returns.
	// Delivery uses a bounded FIFO and never blocks execution. RunResult.Events
	// retains lifecycle/evidence and bounded progress with explicit omissions.
	// Nil disables live delivery.
	LiveEvents chan<- events.Event
	// LogRedactions are exact byte strings replaced in log events. Redaction
	// works across writes and is independent for stdout and stderr.
	LogRedactions []string
}

type completedNode struct {
	baseline     *gitstate.Baseline
	id           string
	attemptID    events.AttemptID
	outputs      map[string]graph.Value
	evidence     *events.NodeEvidencePayload
	check        *events.GateCheckPayload
	checkInvalid bool
	diagnostics  []graph.Diagnostic
	err          error
}

const maxEventLogChunk = 16 * 1024

type runMachine struct {
	units       unitRegistry
	workspaces  map[string]workspaceIdentity
	definition  *graph.Definition
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
	redactions := append([]string(nil), options.LogRedactions...)
	redactionTemplate, err := newLogRedactor(redactions)
	if err != nil {
		return RunResult{}, fmt.Errorf("invalid log redactions: %w", err)
	}

	// Byte logs serialize as base64, but normalized/evidence string observation
	// must not let JSON manufacture replacement runes after secret redaction.
	if _, observed := executor.(ObservedExecutor); observed {
		for _, value := range redactions {
			if !utf8.ValidString(value) {
				return RunResult{}, errors.New("invalid observed redactions: values must be valid UTF-8")
			}
		}
		if !utf8.Valid(redactionTemplate.marker) {
			return RunResult{}, errors.New("invalid observed redactions: no UTF-8-safe secret-absent marker is available")
		}
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

	scopes, scopeRedacted, err := unitScopes(definition, redactionTemplate)
	if err != nil {
		return RunResult{}, err
	}
	if err := preflightLoops(ctx, definition, executor); err != nil {
		return RunResult{}, err
	}
	machine, err := newRunMachine(ctx, definition, options.LiveEvents)
	if err != nil {
		return RunResult{}, err
	}
	machine.definition = definition
	for _, id := range definition.Units {
		if err := machine.units.register(id, machine.result.RunID, machine.nodes[id].NodeRunID, machine.result.GraphVersion, scopes[id], scopeRedacted[id]); err != nil {
			return RunResult{}, err
		}
	}
	if definition.Units != nil {
		machine.workspaces = inspectWorkspaces(ctx, definition)
		machine.reportScopeConflicts(definition)
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

		attempts := map[string]events.AttemptID{}
		numbers := map[string]int{}
		feedbacks := map[string]*CorrectionFeedback{}
		baselines := map[string]*gitstate.Baseline{}
		for len(batch) > 0 {
			// Only workers run concurrently; the coordinator owns every state and
			// output map. Buffering lets all workers return before result collection.
			completed := make(chan completedNode, len(batch))
			started := 0
			for _, id := range batch {
				if ctx.Err() != nil {
					break
				}
				if machine.nodes[id].State == NodeReady {
					machine.transitionNode(id, NodeRunning)
				}
				if l := definition.Nodes[id].Loop; l != nil {
					aid, e := events.NewAttemptID()
					if e != nil {
						machine.nodes[id].Failure = &Failure{NodeID: id, Kind: FailureScheduler, Err: e}
						machine.transitionNode(id, NodeFailed)
						continue
					}
					if machine.units[id] != nil {
						if e := machine.units.begin(id, aid); e != nil {
							machine.nodes[id].Failure = &Failure{NodeID: id, Kind: FailureScheduler, Err: e}
							machine.transitionNode(id, NodeFailed)
							continue
						}
					}
					attempts[id] = aid
					numbers[id]++
					machine.appendEvent(events.Event{Type: events.AttemptStarted, NodeID: id, NodeRunID: machine.nodes[id].NodeRunID, AttemptID: aid, Payload: events.AttemptStartedPayload{Number: numbers[id], MaxAttempts: l.Attempts()}})
					machine.appendEvent(events.Event{Type: events.AttemptInput, NodeID: id, NodeRunID: machine.nodes[id].NodeRunID, AttemptID: aid, Payload: attemptInput(definition.Nodes[id], machine.resolved[id], feedbacks[id], redactionTemplate)})
				}
				node := definition.Nodes[id]
				resolved := machine.resolved[id]
				aid, feedback, baseline, number := attempts[id], feedbacks[id], baselines[id], numbers[id]
				go func(id string) {
					completed <- machine.cycle(ctx, executor, id, node, resolved, aid, number, feedback, baseline, redactionTemplate)
				}(id)
				started++
			}
			results := make([]completedNode, 0, started)
			for range started {
				results = append(results, <-completed)
			}
			sort.Slice(results, func(i, j int) bool { return results[i].id < results[j].id })
			var retry []string
			for _, work := range results {
				if work.baseline != nil {
					baselines[work.id] = work.baseline
				}
				nodeResult := machine.nodes[work.id]
				if definition.Nodes[work.id].Loop != nil {
					outcome := "execution_error"
					if work.err != nil && ctx.Err() != nil && (errors.Is(work.err, context.Canceled) || errors.Is(work.err, context.DeadlineExceeded)) {
						outcome = "cancelled"
					}
					if work.check != nil && !work.checkInvalid {
						machine.appendEvent(events.Event{Type: events.GateCheck, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: events.CloneGate(*work.check)})
						if machine.units[work.id] != nil {
							envelope, err := machine.units.envelope(work.id, work.attemptID, *work.check)
							if err == nil {
								err = machine.units.publish(envelope, *work.check, work.outputs)
							}
							if err != nil {
								work.err = err
								work.outputs = nil
								work.check = nil
								work.checkInvalid = true
							} else {
								machine.appendEvent(events.Event{Type: events.ReturnEnvelope, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: envelope})
							}
						}
						if work.check != nil && work.check.Verdict == "red" {
							outcome = "rejected"
							if numbers[work.id] >= definition.Nodes[work.id].Loop.Attempts() {
								outcome = "needs_intervention"
							}
							if ctx.Err() != nil {
								outcome = "cancelled"
								work.err = ctx.Err()
							}
						}
						if work.check != nil && work.check.Verdict == "green" {
							outcome = "accepted"
						}
					}
					if work.evidence != nil {
						machine.appendEvent(events.Event{Type: events.NodeEvidence, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: *work.evidence})
						work.evidence = nil
					}
					checkID := ""
					if work.check != nil {
						checkID = work.check.CheckID
					}
					machine.appendEvent(events.Event{Type: events.AttemptFinished, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, AttemptID: work.attemptID, Payload: events.AttemptFinishedPayload{Outcome: outcome, CheckID: checkID}})
					if outcome == "rejected" && ctx.Err() == nil {
						feedbacks[work.id] = &CorrectionFeedback{PreviousAttemptID: work.attemptID, CheckResult: events.CloneGate(*work.check), IntendedScope: originalScope(definition.Nodes[work.id])}
						retry = append(retry, work.id)
						continue
					}
					if outcome == "needs_intervention" {
						nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureNeedsIntervention, Err: interventionError{}}
						machine.transitionNode(work.id, NodeFailed)
						continue
					}
					if outcome == "rejected" {
						work.err = ctx.Err()
					}
					work.check = nil
					work.checkInvalid = false
				}
				if work.check != nil || work.checkInvalid {
					valid := !work.checkInvalid && work.check != nil && definition.Nodes[work.id].Type == "gate" && work.evidence == nil
					if valid && work.check.Verdict == "red" {
						var verdict interface{ GateVerdict() string }
						valid = errors.As(work.err, &verdict) && verdict.GateVerdict() == "red" && len(work.outputs) == 0 && !errors.Is(work.err, context.Canceled) && !errors.Is(work.err, context.DeadlineExceeded)
					}
					if valid && work.check.Verdict == "green" {
						valid = work.err == nil && len(validateNodeOutputs(definition.ID, work.id, definition.Nodes[work.id].Outputs, work.outputs)) == 0
						for k, v := range work.outputs {
							t, ok := v.AsString()
							valid = valid && k == "verdict" && ok && t == "green"
						}
					}
					if !valid {
						work.err = errors.New("invalid or inconsistent gate observation")
						work.outputs = nil
					} else {
						machine.appendEvent(events.Event{Type: events.GateCheck, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, Payload: *work.check})
					}
				} else if definition.Nodes[work.id].Type == "gate" && work.err == nil {
					work.err = errors.New("gate execution missing check observation")
					work.outputs = nil
				}
				if work.err != nil {
					if ctx.Err() != nil && (errors.Is(work.err, context.Canceled) || errors.Is(work.err, context.DeadlineExceeded)) {
						machine.transitionNode(work.id, NodeCancelled)
					} else {
						nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureExecutor, Err: work.err}
						if len(work.diagnostics) > 0 {
							nodeResult.Failure.Kind = FailureOutputContract
							nodeResult.Failure.Diagnostics = work.diagnostics
						}
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
				if u := machine.units[work.id]; u != nil {
					values, ports, err := machine.units.acceptedValues(work.id)
					if err != nil {
						nodeResult.Failure = &Failure{NodeID: work.id, Kind: FailureScheduler, Err: err}
						machine.transitionNode(work.id, NodeFailed)
						continue
					}
					machine.outputs[work.id] = values
					nodeResult.Outputs = ports
					nodeResult.AcceptedAttemptID = u.accepted
				}
				if work.evidence != nil {
					machine.appendEvent(events.Event{Type: events.NodeEvidence, NodeID: work.id, NodeRunID: nodeResult.NodeRunID, Payload: *work.evidence})
				}
				machine.transitionNode(work.id, NodeSucceeded)
			}
			failed := false
			for _, id := range machine.ids {
				if machine.nodes[id].State == NodeFailed {
					failed = true
				}
			}
			if failed || ctx.Err() != nil {
				for _, id := range retry {
					machine.transitionNode(id, NodeCancelled)
				}
				break
			}
			batch = retry
		}
		for _, id := range machine.ids {
			if machine.nodes[id].State == NodeRunning {
				machine.transitionNode(id, NodeCancelled)
			}
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

func newRunMachine(ctx context.Context, definition *graph.Definition, live chan<- events.Event) (*runMachine, error) {
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
		units:       unitRegistry{},
		nodes:       make(map[string]*NodeResult, len(ids)),
		resolved:    make(map[string]map[string]graph.Value, len(ids)),
		outputs:     make(map[string]map[string]graph.Value, len(ids)),
		result:      RunResult{GraphID: definition.ID, GraphVersion: version, RunID: runID, State: RunRunning},
		nodeStarted: make(map[string]time.Time, len(ids)),
	}
	for _, id := range ids {
		nodeRunID, err := events.NewNodeRunID()
		if err != nil {
			return nil, err
		}
		m.nodes[id] = &NodeResult{NodeID: id, NodeRunID: nodeRunID, State: NodePending}
	}
	m.emitter = events.NewCollectorWithLiveEvents(ctx, runID, definition.ID, version, live)
	m.record("", "", string(RunRunning))
	return m, nil
}

func (m *runMachine) appendEvent(event events.Event) events.Event {
	recorded, err := m.emitter.Append(event)
	if err != nil {
		panic(fmt.Errorf("scheduler event invariant: %w", err))
	}
	return recorded
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
	recorded := m.appendEvent(event)
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
	// Executors may expose a fixed safe code; raw error text is never an event.
	var diagnostic interface{ DiagnosticCode() string }
	if errors.As(failure.Err, &diagnostic) {
		if code := diagnostic.DiagnosticCode(); code != "" {
			codes = append(codes, code)
		}
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
			conflict := false
			if m.workspaces != nil && m.definition.Nodes[id].Type == "agent" {
				for _, other := range batch {
					if m.definition.Nodes[other].Type == "agent" && workspaceConflict(m.workspaces[id], m.workspaces[other]) {
						conflict = true
						break
					}
				}
			}
			if conflict {
				continue
			}
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
	result.ProgressStats = m.emitter.Stats()
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

func (m *runMachine) invoke(ctx context.Context, executor Executor, id string, node graph.NodeDefinition, resolved map[string]graph.Value, attemptID events.AttemptID, attemptNumber int, feedback *CorrectionFeedback, baseline *gitstate.Baseline, baselineError error, redactionTemplate *logRedactor) completedNode {
	nodeRunID := m.nodes[id].NodeRunID
	var outputs map[string]graph.Value
	var err error
	var evidence *events.NodeEvidencePayload
	var check *events.GateCheckPayload
	checkInvalid := false
	streaming, hasLogs := executor.(LogStreamingExecutor)
	observed, hasObserved := executor.(ObservedExecutor)
	if hasLogs || hasObserved {
		redactor := redactionTemplate.clone()
		var logMu sync.Mutex
		closed := false
		emitLog := func(stream events.Stream, data []byte) {
			for len(data) > 0 {
				chunk := data
				if len(chunk) > maxEventLogChunk {
					chunk = chunk[:maxEventLogChunk]
				}
				m.appendEvent(events.Event{Type: events.LogChunk, NodeID: id, NodeRunID: nodeRunID, AttemptID: attemptID, Payload: events.LogChunkPayload{Stream: stream, Data: chunk}})
				data = data[len(chunk):]
			}
		}
		progress := newProgressRedactor(redactionTemplate, func(kind events.Type, payload events.Payload) {
			m.appendEvent(events.Event{Type: kind, NodeID: id, NodeRunID: nodeRunID, AttemptID: attemptID, Payload: payload})
		})
		observerProgress := func(kind events.Type, payload events.Payload) {
			logMu.Lock()
			defer logMu.Unlock()
			if !closed {
				progress.write(kind, payload)
			}
		}
		observer := func(stream string, data []byte) {
			if stream != string(events.Stdout) && stream != string(events.Stderr) {
				return
			}
			logMu.Lock()
			if closed {
				logMu.Unlock()
				return
			}
			emitLog(events.Stream(stream), redactor.Write(events.Stream(stream), data))
			logMu.Unlock()
		}
		observeCheck := func(p events.GateCheckPayload) {
			logMu.Lock()
			defer logMu.Unlock()
			if closed {
				return
			}
			if check != nil || p.Validate() != nil || p.CheckID != node.Config["check_id"] || string(p.Kind) != node.Config["kind"] || (attemptID != "" && !checkScopeMatches(node, p)) {
				checkInvalid = true
				return
			}
			snapshot := events.CloneGate(p)
			redact := func(s string) string { return progress.redact(s) }
			snapshot.CheckID = redact(snapshot.CheckID)
			snapshot.Reason = redact(snapshot.Reason)
			if snapshot.ChangedFiles != nil {
				for i, path := range snapshot.ChangedFiles.AllowedPaths {
					snapshot.ChangedFiles.AllowedPaths[i] = redact(path)
					snapshot.ChangedFiles.ScopeRedacted = snapshot.ChangedFiles.ScopeRedacted || snapshot.ChangedFiles.AllowedPaths[i] != path
				}
				sort.Strings(snapshot.ChangedFiles.AllowedPaths)
				if snapshot.ChangedFiles.ScopeRedacted {
					unique := snapshot.ChangedFiles.AllowedPaths[:0]
					for _, path := range snapshot.ChangedFiles.AllowedPaths {
						if len(unique) == 0 || unique[len(unique)-1] != path {
							unique = append(unique, path)
						}
					}
					snapshot.ChangedFiles.AllowedPaths = unique
				}
			}
			for i := range snapshot.Scope.CheckedInputs {
				snapshot.Scope.CheckedInputs[i] = redact(snapshot.Scope.CheckedInputs[i])
			}
			sort.Strings(snapshot.Scope.CheckedInputs)
			for i := range snapshot.Evidence {
				e := &snapshot.Evidence[i]
				e.Path = redact(e.Path)
				if e.Reference != nil {
					e.Reference.Identifier = redact(e.Reference.Identifier)
					e.Reference.Reason = redact(e.Reference.Reason)
				}
				// Byte evidence is redacted again at the trust boundary for custom executors.
				r := redactionTemplate.clone()
				b := append([]byte(nil), r.Write(events.Stdout, e.Stdout)...)
				e.Stdout = append(b, r.Flush(events.Stdout)...)
				r = redactionTemplate.clone()
				b = append([]byte(nil), r.Write(events.Stderr, e.Stderr)...)
				e.Stderr = append(b, r.Flush(events.Stderr)...)
			}
			if snapshot.Kind == events.CheckChangedFilesScope {
				sort.SliceStable(snapshot.Evidence, func(i, j int) bool {
					if snapshot.Evidence[i].Path == "" {
						return false
					}
					if snapshot.Evidence[j].Path == "" {
						return true
					}
					return snapshot.Evidence[i].Path < snapshot.Evidence[j].Path
				})
				// Redaction can expand path text after the Gate has applied its
				// payload budget. Preserve a bounded, valid check by replacing
				// excess path entries with a counted omission marker.
				for {
					encoded, marshalErr := json.Marshal(snapshot)
					if marshalErr != nil || len(encoded) <= events.MaxGatePayload {
						break
					}
					lastPath := -1
					for i := len(snapshot.Evidence) - 1; i >= 0; i-- {
						if snapshot.Evidence[i].Path != "" {
							lastPath = i
							break
						}
					}
					if lastPath < 0 {
						break
					}
					snapshot.Evidence = append(snapshot.Evidence[:lastPath], snapshot.Evidence[lastPath+1:]...)
					last := len(snapshot.Evidence) - 1
					if last >= 0 && snapshot.Evidence[last].Path == "" && snapshot.Evidence[last].Omitted > 0 {
						snapshot.Evidence[last].Omitted++
					} else {
						snapshot.Evidence = append(snapshot.Evidence, events.GateEvidence{Kind: events.EvidenceKind(events.CheckChangedFilesScope), Source: "git", Omitted: 1, Reference: &events.EvidenceReference{Kind: "diff", State: "unavailable", Reason: "paths_exceed_evidence_limit"}})
					}
				}
			}
			if snapshot.Validate() != nil {
				checkInvalid = true
				return
			}
			check = &snapshot
		}
		if hasObserved {
			outputs, evidence, err = observed.ExecuteNodeObserved(ctx, id, node, resolved, ExecutionObservation{AttemptNumber: attemptNumber, ScopeBaseline: baseline, ScopeBaselineError: baselineError, Feedback: CloneFeedback(feedback), Correlation: ExecutionCorrelation{RunID: m.result.RunID, NodeRunID: nodeRunID, AttemptID: attemptID}, Log: observer, Progress: observerProgress, Check: observeCheck, NewCheckCapture: func() *CheckCapture { return newCheckCapture(redactionTemplate) }})
		} else {
			outputs, err = streaming.ExecuteNodeWithLogs(ctx, id, node, resolved, observer)
		}
		logMu.Lock()
		emitLog(events.Stdout, redactor.Flush(events.Stdout))
		emitLog(events.Stderr, redactor.Flush(events.Stderr))
		progress.flush()
		closed = true
		logMu.Unlock()
	} else if correlated, ok := executor.(CorrelatedExecutor); ok {
		outputs, err = correlated.ExecuteNodeWithCorrelation(ctx, id, node, resolved, ExecutionCorrelation{RunID: m.result.RunID, NodeRunID: nodeRunID, AttemptID: attemptID})
	} else {
		outputs, err = executor.ExecuteNode(ctx, id, node, resolved)
	}
	if evidence != nil {
		snapshot := *evidence
		snapshot.ChangedFiles = append([]events.ChangedFile{}, evidence.ChangedFiles...)
		redact := func(s string) string {
			r := redactionTemplate.clone()
			data := append([]byte{}, r.Write(events.Stdout, []byte(s))...)
			data = append(data, r.Flush(events.Stdout)...)
			return string(data)
		}
		snapshot.AdapterID = redact(snapshot.AdapterID)
		snapshot.AdapterVersion = redact(snapshot.AdapterVersion)
		for i := range snapshot.ChangedFiles {
			snapshot.ChangedFiles[i].Path = redact(snapshot.ChangedFiles[i].Path)
		}
		evidence = &snapshot
	}
	return completedNode{id: id, attemptID: attemptID, outputs: outputs, evidence: evidence, check: check, checkInvalid: checkInvalid, err: err}
}
