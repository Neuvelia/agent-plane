package run

import "agentplane/internal/domain/events"

// AcceptedProvenance identifies the immutable runtime value, not a file snapshot.
type AcceptedProvenance struct {
	RunID     events.RunID     `json:"run_id"`
	NodeRunID events.NodeRunID `json:"node_run_id"`
	AttemptID events.AttemptID `json:"accepted_attempt_id"`
	Port      string           `json:"port"`
}
