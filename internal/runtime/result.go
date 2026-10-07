package runtime

import (
	"fmt"

	"agentplane/internal/graph"
)

// FailureKind identifies which in-memory execution contract failed.
type FailureKind string

const (
	FailureExecutor       FailureKind = "executor"
	FailureBinding        FailureKind = "binding"
	FailureOutputContract FailureKind = "output_contract"
	FailureGraphOutput    FailureKind = "graph_output"
	FailureScheduler      FailureKind = "scheduler"
)

// Failure preserves the error cause or structured diagnostics within the
// local run result. F0.6 will define any serialized event representation.
type Failure struct {
	NodeID      string
	Kind        FailureKind
	Err         error
	Diagnostics []graph.Diagnostic
}

// PortValue keeps externally presented values in a stable name order.
type PortValue struct {
	Name  string
	Value graph.Value
}

// NodeResult describes the terminal outcome of one node.
type NodeResult struct {
	NodeID     string
	State      NodeState
	Outputs    []PortValue
	Failure    *Failure
	SkipReason string
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
	GraphID     string
	State       RunState
	Nodes       []NodeResult
	Outputs     []PortValue
	Failure     *Failure
	Transitions []StateTransition
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
