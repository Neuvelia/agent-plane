package run

import (
	"fmt"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
)

// FailureKind identifies which in-memory execution contract failed.
type FailureKind string

// Failure kinds, from user-actionable to internal.
const (
	FailureNeedsIntervention FailureKind = "needs_intervention"
	FailureExecutor          FailureKind = "executor"
	FailureRejected          FailureKind = "rejected"
	FailureBinding           FailureKind = "binding"
	FailureOutputContract    FailureKind = "output_contract"
	FailureGraphOutput       FailureKind = "graph_output"
	FailureScheduler         FailureKind = "scheduler"
	FailureTimeout           FailureKind = "timeout"
	FailureShutdown          FailureKind = "shutdown"
)

// Failure preserves the error cause or structured diagnostics within the
// local run result. Events expose only the kind and diagnostic codes.
// When the run redacts secrets, Err is a presented error: errors.Is answers from
// the original cause, and errors.As reaches only a masked copy of the first
// error in the chain that has a Redacted method. Types wrapped by that error
// (for example a shell error under an agent error) are not reachable (ADR-0029).
type Failure struct {
	NodeID      string
	Kind        FailureKind
	Err         error
	Diagnostics []graph.Diagnostic
}

// PortValue keeps externally presented values in a stable name order.
//
// A run with an active redaction set (--redact-env or resolved secrets)
// presents copies in Result: accepted values used by dataflow stay raw (F3.8).
type PortValue struct {
	Provenance *AcceptedProvenance
	Name       string
	Value      graph.Value
	// Redacted reports that Value is a masked copy, or was withheld.
	Redacted bool
	// Withheld is the original type of a value that cannot be rewritten
	// safely (number, boolean, JSON, or a mask that is not valid UTF-8). Value
	// is then the zero Value. It is empty for every other port.
	Withheld graph.ValueType
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
	// Reason is set on cancelled nodes and on node_timeout failures.
	Reason CancelReason
}

// StateTransition records an observable in-memory state change. An empty
// NodeID identifies a run transition. No values or error text are recorded.
type StateTransition struct {
	Sequence uint64
	NodeID   string
	From     string
	To       string
}

// Result is ordered by node ID and graph output name. A started run has
// exactly one terminal State when Run returns.
type Result struct {
	// PersistError is the first Options.Persist failure, if any.
	PersistError error
	GraphID      string
	GraphVersion string
	RunID        events.RunID
	State        State
	Nodes        []NodeResult
	Outputs      []PortValue
	Failure      *Failure
	// Reason is set when the run ended cancelled.
	Reason        CancelReason
	Transitions   []StateTransition
	Events        []events.Event
	ProgressStats events.DeliveryStats
	// Isolation is nil unless a node ran with isolation: worktree.
	Isolation *IsolationResult
}

// IsolationResult is the observable outcome of workspace isolation (ADR-0024),
// present only when at least one node ran in its own worktree. Integration
// facts are populated from coordinator state, never from a worker.
type IsolationResult struct {
	BaseSHA string
	// IntegratedRef and IntegratedCommit are empty until a unit was integrated.
	IntegratedRef    string
	IntegratedCommit string
	Units            []IsolatedUnit
	Retained         []RetainedWorkspace
}

// IsolatedUnit is the commit recorded for an accepted unit. Integrated is false
// when the unit conflicted with the integrated result.
type IsolatedUnit struct {
	NodeID     string
	Ref        string
	Commit     string
	Integrated bool
}

// RetainedWorkspace is a worktree that stays on disk, with the closed release
// reason of the workspace_released fact.
type RetainedWorkspace struct {
	NodeID string
	Path   string
	Reason string
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
