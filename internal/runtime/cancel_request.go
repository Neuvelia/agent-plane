package runtime

import (
	"context"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/run"
)

// causeReason reads the typed cancel cause of the run context. Error text and
// the bare context errors never yield a reason.
func (m *runMachine) causeReason() run.CancelReason {
	if m.ctx == nil || m.ctx.Err() == nil {
		return ""
	}
	reason, _ := run.ReasonOf(context.Cause(m.ctx))
	return reason
}

// cancelledNodeReason explains a cancelled node: the run cause, or a failed
// sibling when the run context is still live.
func (m *runMachine) cancelledNodeReason() run.CancelReason {
	if m.ctx != nil && m.ctx.Err() != nil {
		return m.causeReason()
	}
	return run.CancelSiblingFailed
}

// requestRunCancel emits the run-scoped cancel_requested fact once, before any
// process termination or terminal fact, and never after run_finished. A cancel
// without a typed cause (a bare context.Canceled) has no reason and emits nothing.
func (m *runMachine) requestRunCancel() {
	m.cancelMu.Lock()
	defer m.cancelMu.Unlock()
	if m.cancelSent || m.runEnded {
		return
	}
	reason := m.causeReason()
	if reason == "" {
		return
	}
	m.cancelSent = true
	source := events.CancelSourceSignal
	switch reason {
	case run.CancelRunTimeout, run.CancelNodeTimeout:
		source = events.CancelSourceDeadline
	case run.CancelUserCancel:
		source = events.CancelSourceAPI
	case run.CancelSiblingFailed:
		source = events.CancelSourceFailure
	}
	m.appendEvent(events.Event{Type: events.CancelRequested, Payload: events.CancelRequestedPayload{Scope: events.CancelScopeRun, Reason: string(reason), Source: source}})
}
