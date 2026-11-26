package runtime

import (
	"context"
	"errors"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/proctree"
)

// Time limits (F3.4, ADR-0028). timeoutUnit converts the authored integer
// seconds into a duration; tests shorten it. abandonBound is how long the
// scheduler waits for an executor that ignores a timeout cancellation before it
// gives up on it: the default grace plus the verification bound of proctree.
var (
	timeoutUnit  = time.Second
	abandonBound = proctree.DefaultGrace + 2*time.Second
)

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
// fn still has not returned after abandonBound, the call is abandoned: the
// goroutine is left to finish on its own and its late result is discarded.
func callBounded(execCtx context.Context, fn func() execOutcome) (execOutcome, bool) {
	done := make(chan execOutcome, 1)
	go func() { done <- fn() }()
	select {
	case o := <-done:
		return o, false
	case <-execCtx.Done():
	}
	if reason, _ := run.ReasonOf(context.Cause(execCtx)); reason != run.CancelNodeTimeout && reason != run.CancelRunTimeout {
		return <-done, false
	}
	timer := time.NewTimer(abandonBound)
	defer timer.Stop()
	select {
	case o := <-done:
		return o, false
	case <-timer.C:
		return execOutcome{err: abandonedError{}}, true
	}
}

// nodeTimedOut reports whether the node deadline, not the run, ended the call.
func nodeTimedOut(runCtx, execCtx context.Context) bool {
	if runCtx.Err() != nil {
		return false
	}
	reason, ok := run.ReasonOf(context.Cause(execCtx))
	return ok && reason == run.CancelNodeTimeout
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
