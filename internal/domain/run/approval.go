package run

import "agentplane/internal/domain/events"

// ApprovalRequest is what a DecisionProvider shows to a person. Prompt and
// Context are already redacted and bounded.
type ApprovalRequest struct {
	RunID     events.RunID
	NodeID    string
	NodeRunID events.NodeRunID
	Prompt    string
	Context   []events.ApprovalContext
}

// Decision is a human's answer. Comment is bounded by the event contract.
type Decision struct {
	Approve bool
	Comment string
}
