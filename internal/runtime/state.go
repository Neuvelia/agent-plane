package runtime

// NodeState is the lifecycle of one node in an in-memory run.
type NodeState string

const (
	NodePending   NodeState = "pending"
	NodeReady     NodeState = "ready"
	NodeRunning   NodeState = "running"
	NodeSucceeded NodeState = "succeeded"
	NodeFailed    NodeState = "failed"
	NodeSkipped   NodeState = "skipped"
	NodeCancelled NodeState = "cancelled"
)

// RunState is the lifecycle of one started graph run.
type RunState string

const (
	RunRunning   RunState = "running"
	RunSucceeded RunState = "succeeded"
	RunFailed    RunState = "failed"
	RunCancelled RunState = "cancelled"
)

func validNodeTransition(from, to NodeState) bool {
	switch from {
	case NodePending:
		return to == NodeReady || to == NodeFailed || to == NodeSkipped || to == NodeCancelled
	case NodeReady:
		return to == NodeRunning || to == NodeSkipped || to == NodeCancelled
	case NodeRunning:
		return to == NodeSucceeded || to == NodeFailed || to == NodeCancelled
	default:
		return false
	}
}

func validRunTransition(from, to RunState) bool {
	return from == RunRunning && (to == RunSucceeded || to == RunFailed || to == RunCancelled)
}
