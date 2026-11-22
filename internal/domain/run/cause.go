package run

import (
	"errors"

	"agentplane/internal/domain/events"
)

// CancelReason is the closed vocabulary of why a run or node was stopped. It
// mirrors the reason carried by terminal events (ADR-0028).
type CancelReason string

// The five reasons, equal to the strings carried by terminal events.
const (
	CancelInterrupt     CancelReason = events.ReasonInterrupt
	CancelUserCancel    CancelReason = events.ReasonUserCancel
	CancelRunTimeout    CancelReason = events.ReasonRunTimeout
	CancelSiblingFailed CancelReason = events.ReasonSiblingFailed
	CancelNodeTimeout   CancelReason = events.ReasonNodeTimeout
)

// Valid reports whether r belongs to the closed vocabulary.
func (r CancelReason) Valid() bool { return events.ValidReason(string(r)) }

// CancelCause is the typed value passed to context.WithCancelCause. Node names
// the node a node-scoped cause belongs to; it is empty for run causes.
type CancelCause struct {
	Reason CancelReason
	Node   string
}

func (c CancelCause) Error() string { return "cancelled: " + string(c.Reason) }

// ReasonOf reads a reason from a typed CancelCause anywhere in the error chain.
// Error text, context.Canceled and context.DeadlineExceeded never yield one.
func ReasonOf(cause error) (CancelReason, bool) {
	var value CancelCause
	var pointer *CancelCause
	switch {
	case errors.As(cause, &value):
	case errors.As(cause, &pointer) && pointer != nil:
		value = *pointer
	default:
		return "", false
	}
	if !value.Reason.Valid() {
		return "", false
	}
	return value.Reason, true
}
