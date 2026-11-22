package events

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	AttemptStarted  Type = "attempt_started"
	AttemptInput    Type = "attempt_input"
	AttemptFinished Type = "attempt_finished"
)

type AttemptStartedPayload struct {
	Number      int `json:"number"`
	MaxAttempts int `json:"max_attempts"`
}

// AttemptInputPayload is an observable summary, not a replayable provider input.
type AttemptInputPayload struct {
	TaskFingerprint        string    `json:"task_fingerprint"`
	IntendedScope          []string  `json:"intended_scope"`
	ScopePresent           bool      `json:"scope_present"`
	ContextRefsPresent     bool      `json:"context_refs_present"`
	EnvironmentRefsPresent bool      `json:"environment_refs_present"`
	ReferencesOmitted      bool      `json:"references_omitted"`
	ScopeOmitted           bool      `json:"scope_omitted"`
	PreviousAttemptID      AttemptID `json:"previous_attempt_id,omitempty"`
	PreviousCheckID        string    `json:"previous_check_id,omitempty"`
}
type AttemptFinishedPayload struct {
	Outcome string `json:"outcome"`
	CheckID string `json:"check_id,omitempty"`
}

func (AttemptStartedPayload) eventPayload()  {}
func (AttemptInputPayload) eventPayload()    {}
func (AttemptFinishedPayload) eventPayload() {}
func validateAttempt(e Event) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < 6 {
		return errors.New("attempt facts require schema 6")
	}
	if e.AttemptID == "" || e.NodeRunID == "" {
		return errors.New("attempt fact requires attempt and node-run IDs")
	}
	switch p := e.Payload.(type) {
	case AttemptStartedPayload:
		if p.Number < 1 || p.Number > p.MaxAttempts || p.MaxAttempts > 10 {
			return errors.New("invalid attempt budget")
		}
	case AttemptInputPayload:
		hash, err := hex.DecodeString(p.TaskFingerprint)
		if err != nil || len(hash) != 32 || strings.ToLower(p.TaskFingerprint) != p.TaskFingerprint {
			return errors.New("invalid task fingerprint")
		}
		if len(p.TaskFingerprint) != 64 || !p.ScopePresent || (p.PreviousAttemptID == "") != (p.PreviousCheckID == "") {
			return errors.New("invalid attempt input summary")
		}
		if len(p.IntendedScope) > 32 {
			return errors.New("too many intended scope entries")
		}
		size := 0
		for _, s := range p.IntendedScope {
			if !utf8.ValidString(s) || len(s) > 512 {
				return errors.New("invalid intended scope entry")
			}
			size += len(s)
		}
		if size > 1024 {
			return errors.New("intended scope exceeds summary budget")
		}
		b, _ := json.Marshal(p)
		if len(b) > 8192 {
			return errors.New("attempt input summary too large")
		}
	case AttemptFinishedPayload:
		switch p.Outcome {
		case "accepted", "rejected", "needs_intervention":
			if p.CheckID == "" {
				return errors.New("checked outcome requires check ID")
			}
		case "execution_error", "cancelled":
		case "timeout":
			if err := requireCancelSchema(e, "timeout attempt outcome"); err != nil {
				return err
			}
		default:
			return errors.New("invalid attempt outcome")
		}
	}
	return nil
}

// AttemptCheckResults filters detached full results without inventing old attempts.
func AttemptCheckResults(trace []Event, run RunID, node NodeRunID, attempt AttemptID) []CheckResult {
	if attempt == "" {
		return nil
	}
	var selected []Event
	for _, e := range trace {
		if e.AttemptID == attempt {
			selected = append(selected, e)
		}
	}
	return CheckResults(selected, run, node)
}
