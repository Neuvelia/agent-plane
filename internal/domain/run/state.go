package run

// NodeState is the lifecycle of one node in an in-memory run.
type NodeState string

// Node lifecycle states. Terminal states are Succeeded, Failed, Skipped and
// Cancelled.
const (
	NodePending   NodeState = "pending"
	NodeReady     NodeState = "ready"
	NodeRunning   NodeState = "running"
	NodeSucceeded NodeState = "succeeded"
	NodeFailed    NodeState = "failed"
	NodeSkipped   NodeState = "skipped"
	NodeCancelled NodeState = "cancelled"
)

// State is the lifecycle of one started graph run.
type State string

// Run lifecycle states. A started run has exactly one terminal state.
const (
	RunRunning   State = "running"
	RunSucceeded State = "succeeded"
	RunFailed    State = "failed"
	RunCancelled State = "cancelled"
)

// ValidNodeTransition reports whether a node may move from one state to another.
func ValidNodeTransition(from, to NodeState) bool {
	for _, allowed := range nodeTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

var nodeTransitions = map[NodeState][]NodeState{
	NodePending: {NodeReady, NodeFailed, NodeSkipped, NodeCancelled},
	NodeReady:   {NodeRunning, NodeSkipped, NodeCancelled},
	NodeRunning: {NodeSucceeded, NodeFailed, NodeCancelled},
}

// ValidRunTransition reports whether a run may move from one state to another.
func ValidRunTransition(from, to State) bool {
	return from == RunRunning && (to == RunSucceeded || to == RunFailed || to == RunCancelled)
}
