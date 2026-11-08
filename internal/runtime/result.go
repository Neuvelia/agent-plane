package runtime

import (
	"fmt"

	"agentplane/internal/events"
	"agentplane/internal/graph"
)

// FailureKind identifies which in-memory execution contract failed.
type FailureKind string

const (
	FailureNeedsIntervention FailureKind = "needs_intervention"
	FailureExecutor          FailureKind = "executor"
	FailureBinding           FailureKind = "binding"
	FailureOutputContract    FailureKind = "output_contract"
	FailureGraphOutput       FailureKind = "graph_output"
	FailureScheduler         FailureKind = "scheduler"
)

// Failure preserves the error cause or structured diagnostics within the
// local run result. Events expose only the kind and diagnostic codes.
type Failure struct {
	NodeID      string
	Kind        FailureKind
	Err         error
	Diagnostics []graph.Diagnostic
}

// PortValue keeps externally presented values in a stable name order.
type PortValue struct {
	Provenance *AcceptedProvenance
	Name       string
	Value      graph.Value
}

// NodeResult describes the terminal outcome of one node.
type NodeResult struct {
	AcceptedAttemptID events.AttemptID
	NodeID            string
	NodeRunID         events.NodeRunID
	State             NodeState
	Outputs           []PortValue
	Failure           *Failure
	SkipReason        string
}

// AttemptOutcome is the terminal outcome of a bounded Agent loop attempt.
type AttemptOutcome string

const (
	AttemptAccepted          AttemptOutcome = "accepted"
	AttemptRejected          AttemptOutcome = "rejected"
	AttemptNeedsIntervention AttemptOutcome = "needs_intervention"
	AttemptCancelled         AttemptOutcome = "cancelled"
	AttemptExecutionError    AttemptOutcome = "execution_error"
)

// AttemptCheckReference points to a typed Check Result fact without retaining
// its reason, evidence, or other potentially sensitive content.
type AttemptCheckReference struct {
	CheckID          string
	Kind             events.CheckKind
	Verdict          events.CheckVerdict
	Sequence         uint64
	IdentityRedacted bool
}

// AttemptEnvelopeReference points to a typed Return Envelope fact without
// duplicating its content.
type AttemptEnvelopeReference struct {
	UnitID           string
	CheckID          string
	Verdict          events.CheckVerdict
	Sequence         uint64
	IdentityRedacted bool
}

// AttemptSummary survives retained-trace progress eviction. The scheduler
// owns this bounded index; it contains no raw executor output or error text.
type AttemptSummary struct {
	AttemptID events.AttemptID
	NodeID    string
	NodeRunID events.NodeRunID
	Outcome   AttemptOutcome
	Check     *AttemptCheckReference
	Envelope  *AttemptEnvelopeReference
}

// StateTransition records an observable in-memory state change. An empty
// NodeID identifies a run transition. No values or error text are recorded.
type StateTransition struct {
	Sequence uint64
	NodeID   string
	From     string
	To       string
}

// RunResult is ordered by node ID and graph output name. A started run has
// exactly one terminal State when Run returns.
type RunResult struct {
	GraphID       string
	GraphVersion  string
	RunID         events.RunID
	State         RunState
	Nodes         []NodeResult
	Attempts      []AttemptSummary
	Outputs       []PortValue
	Failure       *Failure
	Transitions   []StateTransition
	Events        []events.Event
	ProgressStats events.DeliveryStats
}

// PreflightError reports definition or graph-input diagnostics before any
// node has started. It contains no partial run result.
type PreflightError struct {
	Diagnostics graph.ValidationResult
}

func (e *PreflightError) Error() string {
	if e == nil || len(e.Diagnostics.Diagnostics) == 0 {
		return "graph preflight failed"
	}
	first := e.Diagnostics.Diagnostics[0]
	return fmt.Sprintf("graph preflight failed: %s: %s", first.Code, first.Message)
}
