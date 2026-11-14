package events

import (
	"errors"
	"fmt"
)

// Approval event limits are part of the contract (ADR-0017): prompt, context
// and comment are untrusted text and must stay bounded.
const (
	MaxApprovalPrompt       = 1024
	MaxApprovalComment      = 2048
	MaxApprovalContextItems = 8
	MaxApprovalContextValue = 1024
	ApprovalApproved        = "approved"
	ApprovalRejected        = "rejected"
)

// ApprovalRequested and ApprovalDecided exist from event schema 9. A human
// node waits exactly between them; "waiting_human" is derived from that pair
// rather than being a scheduler state (ADR-0017).
const (
	ApprovalRequested Type = "approval_requested"
	ApprovalDecided   Type = "approval_decided"
)

// ApprovalContext is one bounded, already-redacted declared string input.
type ApprovalContext struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ApprovalRequestedPayload struct {
	Prompt  string            `json:"prompt"`
	Context []ApprovalContext `json:"context"`
}

type ApprovalDecidedPayload struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

func (ApprovalRequestedPayload) eventPayload() {}
func (ApprovalDecidedPayload) eventPayload()   {}

func (p ApprovalRequestedPayload) Validate() error {
	if len(p.Prompt) > MaxApprovalPrompt {
		return errors.New("approval prompt exceeds limit")
	}
	if p.Context == nil {
		return errors.New("approval context must be a list")
	}
	if len(p.Context) > MaxApprovalContextItems {
		return errors.New("approval context exceeds item limit")
	}
	for _, c := range p.Context {
		if c.Name == "" || len(c.Value) > MaxApprovalContextValue {
			return errors.New("invalid approval context item")
		}
	}
	return nil
}

func (p ApprovalDecidedPayload) Validate() error {
	if p.Decision != ApprovalApproved && p.Decision != ApprovalRejected {
		return fmt.Errorf("unknown approval decision %q", p.Decision)
	}
	if len(p.Comment) > MaxApprovalComment {
		return errors.New("approval comment exceeds limit")
	}
	return nil
}

func validateApproval(e Event) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < 9 {
		return errors.New("approval events require schema 9")
	}
	if e.NodeID == "" || e.NodeRunID == "" {
		return errors.New("approval events require node and node run IDs")
	}
	return nil
}
