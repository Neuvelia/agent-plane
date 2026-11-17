package runtime

import (
	"agentplane/internal/app/trace"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/gitstate"
)

// Executor runs one node with resolved inputs. Implementations must respect
// ctx and treat the supplied node definition as read-only. If execution ends
// because of cancellation, the returned error must wrap context.Canceled or
// context.DeadlineExceeded so the scheduler can distinguish it from a failure.
type Executor interface {
	ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error)
}

// CorrelatedExecutor is optional; legacy executors keep the F0.4 API.
type CorrelatedExecutor interface {
	ExecuteNodeWithCorrelation(context.Context, string, graph.NodeDefinition, map[string]graph.Value, run.ExecutionCorrelation) (map[string]graph.Value, error)
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
	Feedback           *run.CorrectionFeedback
	Correlation        run.ExecutionCorrelation
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
	// Decisions serves human nodes. A graph with a human node and no provider
	// fails preflight (ADR-0017).
	Decisions DecisionProvider
	// Persist records every retained event synchronously and in sequence order
	// (F2.9). A failure never stops the run; it is reported in
	// RunResult.PersistError and the caller must treat the run as not durable.
	Persist func(events.Event) error
	// RecordValues emits one bounded, redacted value_recorded fact per supplied
	// graph input and per accepted output port (ADR-0023). Off by default;
	// LogRedactions apply and must be valid UTF-8 when it is on.
	RecordValues bool
	// Workspaces serves nodes with isolation: worktree (ADR-0024). A graph that
	// asks for isolation and has no manager fails preflight; a graph that does
	// not ask never touches it.
	Workspaces WorkspaceManager
	// RetainWorktrees selects which isolated worktrees outlive their node run.
	// The zero value means RetainFailed.
	RetainWorktrees RetainPolicy
	// GraphVersion is the canonical encoding hashed into the run's graph
	// version (graph.Encoder). It is required: the runtime has no serialization
	// format of its own, and the composition root supplies the YAML adapter.
	GraphVersion graph.Encoder
	// Clock stamps events. Required: the engine never reads the wall clock.
	Clock trace.Clock
	// IDs generates run, node-run and attempt identifiers. Required.
	IDs IDGenerator
}

// IDGenerator is the engine's source of identifiers; the composition root
// supplies random ones and tests supply deterministic ones.
type IDGenerator interface {
	NewRunID() (events.RunID, error)
	NewNodeRunID() (events.NodeRunID, error)
	NewAttemptID() (events.AttemptID, error)
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
	isolation   isolationPlan
	worktrees   map[string]*isolatedWorkspace
	definition  *graph.Definition
	ids         []string
	nodes       map[string]*run.NodeResult
	resolved    map[string]map[string]graph.Value
	outputs     map[string]map[string]graph.Value
	result      run.Result
	emitter     *trace.Collector
	startedAt   time.Time
	nodeStarted map[string]time.Time
	decisions   DecisionProvider
	values      *valueRecorder
	attemptIDs  IDGenerator
	clock       trace.Clock
}

// Run executes one validated graph in memory. A non-nil error means preflight
// failed and no run started. Runtime failures are represented in RunResult.
// Callers must not mutate definition or inputs while Run is active.
//
//nolint:gocognit,gocyclo,funlen // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func Run(ctx context.Context, definition *graph.Definition, inputs map[string]graph.Value, executor Executor, options Options) (run.Result, error) {
	prepared, err := preflightRun(ctx, definition, inputs, executor, options)
	if err != nil {
		return run.Result{}, err
	}
	machine, dependencies, err := initializeRun(ctx, definition, options, prepared)
	if err != nil {
		return run.Result{}, err
	}
	plan := prepared.plan
	graphInputs := prepared.graphInputs
	redactionTemplate := prepared.redactionTemplate
	limit := prepared.limit
	if limit > len(machine.ids) {
		limit = len(machine.ids)
	}

	for {
		if ctx.Err() != nil {
			machine.cancelUnstarted()
			machine.finish(ctx, run.RunCancelled)
			return machine.snapshot(), nil
		}
		failures := machine.promoteReady(ctx, plan, graphInputs, dependencies)
		if len(failures) > 0 {
			machine.result.Failure = failures[0]
			machine.skipUnstarted()
			machine.finish(ctx, run.RunFailed)
			return machine.snapshot(), nil
		}
		if ctx.Err() != nil {
			machine.cancelUnstarted()
			machine.finish(ctx, run.RunCancelled)
			return machine.snapshot(), nil
		}

		batch := machine.nextBatch(limit)
		if len(batch) == 0 {
			if machine.allSucceeded() {
				outputs, resolution := plan.ResolveGraphOutputs(machine.outputs)
				if !resolution.Valid() {
					machine.result.Failure = &run.Failure{Kind: run.FailureGraphOutput, Diagnostics: resolution.Diagnostics}
					machine.finish(ctx, run.RunFailed)
				} else {
					machine.result.Outputs = sortedPortValues(outputs)
					machine.finish(ctx, run.RunSucceeded)
				}
			} else {
				machine.result.Failure = &run.Failure{Kind: run.FailureScheduler, Err: errors.New("no runnable nodes remain in validated graph")}
				machine.skipUnstarted()
				machine.finish(ctx, run.RunFailed)
			}
			return machine.snapshot(), nil
		}

		state := batchState{
			attempts:  map[string]events.AttemptID{},
			numbers:   map[string]int{},
			feedbacks: map[string]*run.CorrectionFeedback{},
			baselines: map[string]*gitstate.Baseline{},
		}
		for len(batch) > 0 {
			results := machine.runBatchWorkers(ctx, executor, batch, &state, redactionTemplate)
			retry := machine.acceptBatchResults(ctx, definition, results, &state)
			machine.releaseIsolated(ctx)
			failed := false
			for _, id := range machine.ids {
				if machine.nodes[id].State == run.NodeFailed {
					failed = true
				}
			}
			if failed || ctx.Err() != nil {
				for _, id := range retry {
					machine.transitionNode(id, run.NodeCancelled)
				}
				break
			}
			batch = retry
		}
		for _, id := range machine.ids {
			if machine.nodes[id].State == run.NodeRunning {
				machine.transitionNode(id, run.NodeCancelled)
			}
		}
		for _, id := range machine.ids {
			if node := machine.nodes[id]; node.State == run.NodeFailed {
				machine.result.Failure = node.Failure
				break
			}
		}
		if machine.result.Failure != nil {
			machine.skipUnstarted()
			machine.finish(ctx, run.RunFailed)
			return machine.snapshot(), nil
		}
		if ctx.Err() != nil {
			machine.cancelUnstarted()
			machine.finish(ctx, run.RunCancelled)
			return machine.snapshot(), nil
		}
	}
}

func newRunMachine(ctx context.Context, definition *graph.Definition, options Options) (*runMachine, error) {
	live, persist := options.LiveEvents, options.Persist
	if options.GraphVersion == nil || options.Clock == nil || options.IDs == nil {
		return nil, errors.New("runtime options: GraphVersion, Clock and IDs are required")
	}
	version, err := graph.SemanticVersion(definition, options.GraphVersion)
	if err != nil {
		return nil, err
	}
	runID, err := options.IDs.NewRunID()
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
		nodes:       make(map[string]*run.NodeResult, len(ids)),
		resolved:    make(map[string]map[string]graph.Value, len(ids)),
		outputs:     make(map[string]map[string]graph.Value, len(ids)),
		result:      run.Result{GraphID: definition.ID, GraphVersion: version, RunID: runID, State: run.RunRunning},
		nodeStarted: make(map[string]time.Time, len(ids)),
		attemptIDs:  options.IDs,
		clock:       options.Clock,
		worktrees:   map[string]*isolatedWorkspace{},
	}
	for _, id := range ids {
		nodeRunID, err := options.IDs.NewNodeRunID()
		if err != nil {
			return nil, err
		}
		m.nodes[id] = &run.NodeResult{NodeID: id, NodeRunID: nodeRunID, State: run.NodePending}
	}
	m.emitter = trace.NewCollectorWithLiveEvents(ctx, options.Clock, runID, definition.ID, version, live)
	m.emitter.SetPersist(persist)
	m.record("", "", string(run.RunRunning))
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
				case run.NodeSucceeded:
					counts.Succeeded++
				case run.NodeFailed:
					counts.Failed++
				case run.NodeSkipped:
					counts.Skipped++
				case run.NodeCancelled:
					counts.Cancelled++
				}
			}
			failureKind, _ := eventFailure(m.result.Failure)
			event.Payload = events.RunFinishedPayload{Status: to, DurationMS: m.clock.Now().Sub(m.startedAt).Milliseconds(), NodeCounts: counts, FailureKind: failureKind}
		}
	} else {
		node := m.nodes[id]
		event.NodeRunID = node.NodeRunID
		switch to {
		case string(run.NodeReady):
			event.Type, event.Payload = events.NodeReady, events.NodeReadyPayload{}
		case string(run.NodeRunning):
			event.Type, event.Payload = events.NodeStarted, events.NodeStartedPayload{}
		default:
			failureKind, codes := eventFailure(node.Failure)
			if from == string(run.NodeRunning) {
				duration := m.clock.Now().Sub(m.nodeStarted[id]).Milliseconds()
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
	if id != "" && to == string(run.NodeRunning) {
		m.nodeStarted[id] = recorded.Timestamp
	}
	m.result.Transitions = append(m.result.Transitions, run.StateTransition{Sequence: recorded.Sequence, NodeID: id, From: from, To: to})
}

func eventFailure(failure *run.Failure) (string, []string) {
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

func (m *runMachine) transitionNode(id string, to run.NodeState) {
	from := m.nodes[id].State
	if !run.ValidNodeTransition(from, to) {
		panic(fmt.Sprintf("invalid scheduler node transition %q: %s -> %s", id, from, to))
	}
	m.nodes[id].State = to
	m.record(id, string(from), string(to))
}

func (m *runMachine) finish(ctx context.Context, to run.State) {
	m.releaseIsolated(ctx)
	if !run.ValidRunTransition(m.result.State, to) {
		panic(fmt.Sprintf("invalid scheduler run transition: %s -> %s", m.result.State, to))
	}
	from := m.result.State
	m.result.State = to
	m.record("", string(from), string(to))
}

//nolint:gocognit // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func (m *runMachine) promoteReady(ctx context.Context, plan *graph.BindingPlan, graphInputs map[string]graph.Value, dependencies map[string][]string) []*run.Failure {
	var failures []*run.Failure
	for _, id := range m.ids {
		if ctx.Err() != nil {
			return failures
		}
		if m.nodes[id].State != run.NodePending {
			continue
		}
		ready := true
		for _, predecessor := range dependencies[id] {
			if m.nodes[predecessor].State != run.NodeSucceeded {
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
			failure := &run.Failure{NodeID: id, Kind: run.FailureBinding, Diagnostics: resolution.Diagnostics}
			m.nodes[id].Failure = failure
			m.transitionNode(id, run.NodeFailed)
			failures = append(failures, failure)
			continue
		}
		m.resolved[id] = inputs
		m.transitionNode(id, run.NodeReady)
	}
	return failures
}

//nolint:nestif // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func (m *runMachine) nextBatch(limit int) []string {
	batch := make([]string, 0, limit)
	for _, id := range m.ids {
		if m.nodes[id].State == run.NodeReady {
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
		if m.nodes[id].State != run.NodeSucceeded {
			return false
		}
	}
	return true
}

func (m *runMachine) skipUnstarted() {
	for _, id := range m.ids {
		node := m.nodes[id]
		if node.State == run.NodePending || node.State == run.NodeReady {
			node.SkipReason = "run_failed"
			m.transitionNode(id, run.NodeSkipped)
		}
	}
}

func (m *runMachine) cancelUnstarted() {
	for _, id := range m.ids {
		state := m.nodes[id].State
		if state == run.NodePending || state == run.NodeReady {
			m.transitionNode(id, run.NodeCancelled)
		}
	}
}

func (m *runMachine) snapshot() run.Result {
	result := m.result
	result.Events = m.emitter.Snapshot()
	result.ProgressStats = m.emitter.Stats()
	result.PersistError = m.emitter.PersistError()
	result.Nodes = make([]run.NodeResult, 0, len(m.ids))
	for _, id := range m.ids {
		result.Nodes = append(result.Nodes, *m.nodes[id])
	}
	return result
}

func sortedPortValues(values map[string]graph.Value) []run.PortValue {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]run.PortValue, 0, len(names))
	for _, name := range names {
		result = append(result, run.PortValue{Name: name, Value: values[name]})
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

//nolint:gocognit,gocyclo,funlen // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func (m *runMachine) invoke(ctx context.Context, executor Executor, id string, node graph.NodeDefinition, resolved map[string]graph.Value, attemptID events.AttemptID, attemptNumber int, feedback *run.CorrectionFeedback, baseline *gitstate.Baseline, baselineError error, redactionTemplate *logRedactor) completedNode {
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
			outputs, evidence, err = observed.ExecuteNodeObserved(ctx, id, node, resolved, ExecutionObservation{AttemptNumber: attemptNumber, ScopeBaseline: baseline, ScopeBaselineError: baselineError, Feedback: run.CloneFeedback(feedback), Correlation: run.ExecutionCorrelation{RunID: m.result.RunID, NodeRunID: nodeRunID, AttemptID: attemptID}, Log: observer, Progress: observerProgress, Check: observeCheck, NewCheckCapture: func() *CheckCapture { return newCheckCapture(redactionTemplate) }})
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
		outputs, err = correlated.ExecuteNodeWithCorrelation(ctx, id, node, resolved, run.ExecutionCorrelation{RunID: m.result.RunID, NodeRunID: nodeRunID, AttemptID: attemptID})
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
