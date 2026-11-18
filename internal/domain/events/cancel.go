package events

import (
	"errors"
	"fmt"
)

// Cancellation and timeout facts (F3.4, ADR-0028). They exist from event
// schema 12; older schemas never carry them and stay readable unchanged.
const (
	CancelRequested   Type = "cancel_requested"
	ProcessTerminated Type = "process_terminated"
)

// schemaCancelTimeout is the first schema with these facts. Renumber to 13 if
// F2.8b's schema 12 merges first; tests derive from this constant.
const schemaCancelTimeout = 12

// Closed cause vocabulary shared by run.CancelReason and the terminal payloads.
const (
	ReasonInterrupt     = "interrupt"
	ReasonUserCancel    = "user_cancel"
	ReasonRunTimeout    = "run_timeout"
	ReasonSiblingFailed = "sibling_failed"
	ReasonNodeTimeout   = "node_timeout"
)

// Closed enums of cancel_requested and process_terminated, and payload bounds.
const (
	CancelScopeRun  = "run"
	CancelScopeNode = "node"

	CancelSourceSignal   = "signal"
	CancelSourceAPI      = "api"
	CancelSourceDeadline = "deadline"
	CancelSourceFailure  = "failure"

	ProcessGraceful      = "graceful"
	ProcessForced        = "forced"
	ProcessAlreadyExited = "already_exited"

	// MaxProcessMS bounds grace and elapsed durations (24 hours).
	MaxProcessMS = 24 * 60 * 60 * 1000
	// MaxProcessLeaked bounds the leaked-descendant count.
	MaxProcessLeaked = 100000
)

// CancelRequestedPayload records that a stop was requested, before any
// process is terminated. Scope node requires node correlation on the event.
type CancelRequestedPayload struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
	Source string `json:"source"`
}

// ProcessTerminatedPayload records how a node's process tree ended. It holds
// no paths, environment or command lines. Leaked is non-zero only for forced.
type ProcessTerminatedPayload struct {
	Method    string `json:"method"`
	GraceMS   int64  `json:"grace_ms"`
	ElapsedMS int64  `json:"elapsed_ms"`
	Leaked    int    `json:"leaked"`
}

func (CancelRequestedPayload) eventPayload()   {}
func (ProcessTerminatedPayload) eventPayload() {}

// ValidReason reports whether r is in the closed cause vocabulary.
func ValidReason(r string) bool {
	switch r {
	case ReasonInterrupt, ReasonUserCancel, ReasonRunTimeout, ReasonSiblingFailed, ReasonNodeTimeout:
		return true
	}
	return false
}

func requireCancelSchema(e Event, what string) error {
	if e.SchemaVersion != 0 && e.SchemaVersion < schemaCancelTimeout {
		return fmt.Errorf("%s requires schema %d", what, schemaCancelTimeout)
	}
	return nil
}

// validateTerminalReason checks the optional reason of a terminal fact: it is
// only meaningful on a cancelled status or a timeout failure.
func validateTerminalReason(e Event, status, failureKind, reason string) error {
	if reason == "" {
		return nil
	}
	if err := requireCancelSchema(e, "terminal reason"); err != nil {
		return err
	}
	if !ValidReason(reason) {
		return fmt.Errorf("invalid terminal reason %q", reason)
	}
	if status != "cancelled" && failureKind != "timeout" {
		return errors.New("reason is only valid on a cancelled or timeout-failed terminal")
	}
	return nil
}

func validateCancelRequested(e Event, p CancelRequestedPayload) error {
	if err := requireCancelSchema(e, "cancel_requested"); err != nil {
		return err
	}
	switch p.Scope {
	case CancelScopeRun:
		if e.NodeID != "" || e.NodeRunID != "" {
			return errors.New("run-scoped cancel_requested must not carry node correlation")
		}
	case CancelScopeNode:
		if e.NodeID == "" || e.NodeRunID == "" {
			return errors.New("node-scoped cancel_requested requires node and node-run IDs")
		}
	default:
		return fmt.Errorf("invalid cancel scope %q", p.Scope)
	}
	if !ValidReason(p.Reason) {
		return fmt.Errorf("invalid cancel reason %q", p.Reason)
	}
	switch p.Source {
	case CancelSourceSignal, CancelSourceAPI, CancelSourceDeadline, CancelSourceFailure:
	default:
		return fmt.Errorf("invalid cancel source %q", p.Source)
	}
	return nil
}

func validateProcessTerminated(e Event, p ProcessTerminatedPayload) error {
	if err := requireCancelSchema(e, "process_terminated"); err != nil {
		return err
	}
	if e.NodeID == "" || e.NodeRunID == "" {
		return errors.New("process_terminated requires node and node-run IDs")
	}
	switch p.Method {
	case ProcessGraceful, ProcessForced, ProcessAlreadyExited:
	default:
		return fmt.Errorf("invalid process termination method %q", p.Method)
	}
	return validateProcessCounts(p)
}

func validateProcessCounts(p ProcessTerminatedPayload) error {
	if p.GraceMS < 0 || p.GraceMS > MaxProcessMS || p.ElapsedMS < 0 || p.ElapsedMS > MaxProcessMS {
		return errors.New("process termination duration out of range")
	}
	if p.Leaked < 0 || p.Leaked > MaxProcessLeaked {
		return errors.New("process termination leaked count out of range")
	}
	if p.Leaked > 0 && p.Method != ProcessForced {
		return errors.New("only a forced termination can leak descendants")
	}
	return nil
}
