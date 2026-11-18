package run

import "agentplane/internal/domain/events"

// ExecutionCorrelation is created by the scheduler and passed to executors
// that need to associate external work with the run's lifecycle facts.
type ExecutionCorrelation struct {
	RunID     events.RunID
	NodeRunID events.NodeRunID
	AttemptID events.AttemptID
}
