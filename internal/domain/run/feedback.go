package run

import "agentplane/internal/domain/events"

// CorrectionFeedback preserves the retained sanitized result and the original
// producer scope. Checked Gate ports never redefine the producer's scope.
type CorrectionFeedback struct {
	PreviousAttemptID events.AttemptID   `json:"previous_attempt_id"`
	CheckResult       events.CheckResult `json:"check_result"`
	IntendedScope     []string           `json:"intended_scope"`
}

// CloneFeedback returns a deep copy so retained feedback cannot be mutated by
// an executor.
func CloneFeedback(f *CorrectionFeedback) *CorrectionFeedback {
	if f == nil {
		return nil
	}
	c := *f
	c.CheckResult = events.CloneGate(f.CheckResult)
	c.IntendedScope = append([]string{}, f.IntendedScope...)
	return &c
}
