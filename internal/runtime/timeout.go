package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/proctree"
)

// defaultAbandonBound is how long the scheduler waits for an executor that
// ignores a timeout cancellation before it gives up on it: the default grace
// plus the verification bound of proctree (F3.4, ADR-0028).
const defaultAbandonBound = proctree.DefaultGrace + 2*time.Second

// TimeLimits tunes how the authored limits are applied. The zero value is the
// production behaviour; tests shorten both through Options instead of package
// state (AGENTS.md R8).
type TimeLimits struct {
	// SecondUnit is the duration of one authored second of timeout_seconds and
	// limits.run_timeout_seconds. Zero means time.Second.
	SecondUnit time.Duration
	// AbandonBound is how long an executor that ignores a timeout cancellation
	// is waited for before it is abandoned. Zero means defaultAbandonBound.
	AbandonBound time.Duration
}

func (l TimeLimits) seconds(n int) time.Duration {
	unit := l.SecondUnit
	if unit <= 0 {
		unit = time.Second
	}
	return time.Duration(n) * unit
}

func (l TimeLimits) abandonBound() time.Duration {
	if l.AbandonBound <= 0 {
		return defaultAbandonBound
	}
	return l.AbandonBound
}

// execOutcome is what one executor call returned.
type execOutcome struct {
	outputs  map[string]graph.Value
	evidence *events.NodeEvidencePayload
	err      error
}

// abandonedError reports an executor that did not return within abandonBound
// after a timeout cancellation. The node ends failed with kind shutdown.
type abandonedError struct{}

func (abandonedError) Error() string {
	return "executor did not stop within the termination bound after a timeout"
}

// callBounded runs fn. When the execution context ends with a timeout cause and
// fn still has not returned after bound, the call is abandoned: the goroutine
// is left to finish on its own and its late result is discarded. The returned
// reason is the ignored timeout cause, empty when fn returned.
func callBounded(execCtx context.Context, bound time.Duration, fn func() execOutcome) (execOutcome, run.CancelReason) {
	done := make(chan execOutcome, 1)
	go func() { done <- fn() }()
	select {
	case o := <-done:
		return o, ""
	case <-execCtx.Done():
	}
	reason, _ := run.ReasonOf(context.Cause(execCtx))
	if reason != run.CancelNodeTimeout && reason != run.CancelRunTimeout {
		return <-done, ""
	}
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case o := <-done:
		return o, ""
	case <-timer.C:
		return execOutcome{err: abandonedError{}}, reason
	}
}

// withNodeDeadline returns the context a node's executor call runs under,
// the settle func the call reports its return to, and a stop func to defer.
// Without timeout_seconds the run context is used and settle never fires.
func (m *runMachine) withNodeDeadline(runCtx context.Context, id string, node graph.NodeDefinition, attemptID events.AttemptID) (context.Context, func() bool, func()) {
	if node.TimeoutSeconds == nil {
		return runCtx, func() bool { return false }, func() {}
	}
	ctx, cancelNode := context.WithCancelCause(runCtx)
	request := events.Event{Type: events.CancelRequested, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: attemptID, Payload: events.CancelRequestedPayload{Scope: events.CancelScopeNode, Reason: events.ReasonNodeTimeout, Source: events.CancelSourceDeadline}}
	settle := m.armNodeDeadline(runCtx, m.limits.seconds(*node.TimeoutSeconds), request, func() {
		cancelNode(run.CancelCause{Reason: run.CancelNodeTimeout, Node: id})
	})
	return ctx, settle, func() { settle(); cancelNode(nil) }
}

// reportCallEnd records how an executor call ended when a cancel reached it.
// An abandoned executor (it ignored the cancel cause) gets the run-scoped
// cancel_requested and then executor_abandoned (ADR-0044); a cancelled one that
// returned gets the request and its process termination.
func (m *runMachine) reportCallEnd(id string, attemptID events.AttemptID, err error, ignored run.CancelReason, cancelled bool) {
	if ignored != "" {
		m.requestRunCancel()
		m.reportAbandoned(id, attemptID, ignored)
		return
	}
	if err != nil && cancelled {
		m.requestRunCancel()
		m.reportProcessTermination(id, attemptID, err)
	}
}

// armNodeDeadline starts a node deadline. When it fires while the run is live,
// it records the node-scoped cancel_requested at that moment and then calls
// cancel, so the request precedes any process termination, abandonment or
// terminal fact of the node. The returned settle stops the deadline as soon as
// the executor call has returned and reports whether the deadline fired first.
func (m *runMachine) armNodeDeadline(runCtx context.Context, after time.Duration, request events.Event, cancel func()) func() bool {
	d := &nodeDeadline{record: func() {
		if runCtx.Err() == nil {
			m.appendEvent(request)
		}
	}, cancel: cancel}
	d.timer = time.AfterFunc(after, d.fire)
	return d.settle
}

// nodeDeadline orders a firing deadline against the executor call returning.
// Whichever takes mu first wins: a deadline that fires after settle records
// nothing and cancels nothing, so an ordinary failure returned just before the
// deadline is never reclassified as a timeout.
type nodeDeadline struct {
	mu      sync.Mutex
	settled bool
	fired   bool
	timer   *time.Timer
	record  func()
	cancel  func()
}

func (d *nodeDeadline) fire() {
	d.mu.Lock()
	if d.settled {
		d.mu.Unlock()
		return
	}
	d.fired = true
	d.record()
	d.mu.Unlock()
	d.cancel()
}

// settle stops the deadline and reports whether it fired first. It is
// idempotent.
func (d *nodeDeadline) settle() bool {
	if d.timer != nil {
		d.timer.Stop()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.settled = true
	return d.fired
}

// nodeTimedOut reports whether the node deadline, not the run, ended the call:
// the deadline fired before the call returned and the run is still live.
func nodeTimedOut(runCtx context.Context, deadlineFired bool) bool {
	return deadlineFired && runCtx.Err() == nil
}

// reportProcessTermination records how the node's process tree ended when the
// executor error carries that record (Shell and Agent executors do).
func (m *runMachine) reportProcessTermination(id string, attempt events.AttemptID, err error) {
	var carrier interface{ ProcessTermination() *proctree.Outcome }
	if err == nil || !errors.As(err, &carrier) {
		return
	}
	outcome := carrier.ProcessTermination()
	if outcome == nil {
		return
	}
	payload := events.ProcessTerminatedPayload{Method: string(outcome.Method), GraceMS: outcome.Grace.Milliseconds(), ElapsedMS: outcome.Elapsed.Milliseconds(), Leaked: outcome.Leaked}
	switch payload.Method {
	case events.ProcessGraceful, events.ProcessForced, events.ProcessAlreadyExited:
	default:
		return
	}
	payload.GraceMS = min(max(payload.GraceMS, 0), events.MaxProcessMS)
	payload.ElapsedMS = min(max(payload.ElapsedMS, 0), events.MaxProcessMS)
	payload.Leaked = min(max(payload.Leaked, 0), events.MaxProcessLeaked)
	if payload.Leaked > 0 {
		payload.Method = events.ProcessForced
	}
	m.appendEvent(events.Event{Type: events.ProcessTerminated, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: attempt, Payload: payload})
}

// reportAbandoned records that the executor of node id was abandoned after it
// ignored reason for bound (ADR-0044). Its goroutine and any process it owns
// may outlive run_finished; the fact keeps that from reading as a clean cancel.
func (m *runMachine) reportAbandoned(id string, attempt events.AttemptID, reason run.CancelReason) {
	payload := events.ExecutorAbandonedPayload{Reason: string(reason), BoundMS: min(m.limits.abandonBound().Milliseconds(), events.MaxProcessMS)}
	m.appendEvent(events.Event{Type: events.ExecutorAbandoned, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: attempt, Payload: payload})
}
