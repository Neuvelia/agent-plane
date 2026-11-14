// Package api serves the local, loopback-only control API (ADR-0019). It
// observes and steers runs; it never schedules, retries or decides state.
package api

import (
	"context"
	"errors"
	"sync"
	"unicode/utf8"

	"agentplane/internal/events"
	"agentplane/internal/runtime"
)

// Errors returned by Submit; the HTTP layer maps them to status codes.
var (
	ErrNotPending     = errors.New("no pending approval for this run and node run")
	ErrInvalidComment = errors.New("comment is too long or not valid UTF-8")
)

// PendingApproval is what the approvals inbox shows.
type PendingApproval struct {
	RunID     string                   `json:"run_id"`
	NodeID    string                   `json:"node_id"`
	NodeRunID events.NodeRunID         `json:"node_run_id"`
	Prompt    string                   `json:"prompt"`
	Context   []events.ApprovalContext `json:"context"`
}

// Recorded is the first decision for a node run. It is returned unchanged for
// every later attempt, which is what makes decisions idempotent.
type Recorded struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

type pending struct {
	info PendingApproval
	ch   chan runtime.Decision
}

// LiveRun holds the retained events of a run started by this process so
// streams can replay from sequence zero and then follow without gaps.
type LiveRun struct {
	mu      sync.Mutex
	id      string
	events  []events.Event
	done    bool
	err     string // set when the run could not be persisted; the run is stopped
	changed chan struct{}
}

// Finished reports whether the run reached run_finished or failed to persist.
func (r *LiveRun) Finished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

// Err is the persistence failure that stopped the run, or "".
func (r *LiveRun) Err() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *LiveRun) fail(msg string) {
	r.mu.Lock()
	r.err = msg
	r.done = true
	old := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	close(old)
}

func (r *LiveRun) append(e events.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	if e.Type == events.RunFinished {
		r.done = true
	}
	old := r.changed
	r.changed = make(chan struct{})
	r.mu.Unlock()
	close(old)
}

// From returns events at index >= from, whether the run has finished, and a
// channel closed on the next change. Callers loop: read, send, wait.
func (r *LiveRun) From(from int) ([]events.Event, bool, <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []events.Event
	if from < len(r.events) {
		out = append(out, r.events[from:]...)
	}
	return out, r.done, r.changed
}

// Hub tracks live runs and pending approvals. It implements
// runtime.DecisionProvider for every run served by this process.
type Hub struct {
	mu      sync.Mutex
	runs    map[string]*LiveRun
	pending map[events.NodeRunID]*pending
	decided map[events.NodeRunID]decidedRecord
	// afterRegister is a test seam called once a wait is registered and before
	// Decide blocks. It is nil in production.
	afterRegister func()
	registered    chan struct{} // closed and replaced whenever an approval registers
}

// decidedRecord keeps the run a decision belongs to, so a repeat from another
// run is "not pending" (404) and never reveals this decision.
type decidedRecord struct {
	runID string
	rec   Recorded
}

func NewHub() *Hub {
	return &Hub{runs: map[string]*LiveRun{}, pending: map[events.NodeRunID]*pending{}, decided: map[events.NodeRunID]decidedRecord{}, registered: make(chan struct{})}
}

// Slot is a run that has been requested but whose ID is not known until its
// first event. Sink is the runtime.Options.Persist hook.
type Slot struct {
	hub     *Hub
	next    func(events.Event) error
	live    *LiveRun
	Started chan struct{}
	once    sync.Once
	runID   string
}

// NewSlot chains an optional downstream sink (the run store) before the hub so
// anything the hub can show is already durable.
func (h *Hub) NewSlot(next func(events.Event) error) *Slot {
	return &Slot{hub: h, next: next, Started: make(chan struct{})}
}

// RunID is valid after Started is closed.
func (s *Slot) RunID() string { return s.runID }

func (s *Slot) Sink(e events.Event) error {
	var err error
	if s.next != nil {
		err = s.next(e)
	}
	s.once.Do(func() {
		s.runID = string(e.RunID)
		s.live = &LiveRun{id: s.runID, changed: make(chan struct{})}
		s.hub.mu.Lock()
		s.hub.runs[s.runID] = s.live
		s.hub.mu.Unlock()
		close(s.Started)
	})
	s.live.append(e)
	return err
}

// Fail marks the run as stopped by a persistence failure. Streams end with an
// error event instead of pretending the run completed.
func (s *Slot) Fail(err error) {
	select {
	case <-s.Started:
		s.live.fail(err.Error())
	default:
	}
}

// Live returns the in-memory run, if this process started it.
func (h *Hub) Live(runID string) (*LiveRun, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.runs[runID]
	return r, ok
}

// WaitPending blocks until at least n approvals are pending or ctx ends. It
// lets tests and callers wait on a fact instead of polling.
func (h *Hub) WaitPending(ctx context.Context, n int) error {
	for {
		h.mu.Lock()
		have, changed := len(h.pending), h.registered
		h.mu.Unlock()
		if have >= n {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Pending lists approvals that are waiting right now.
func (h *Hub) Pending() []PendingApproval {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]PendingApproval, 0, len(h.pending))
	for _, p := range h.pending {
		out = append(out, p.info)
	}
	return out
}

// Decide implements runtime.DecisionProvider.
func (h *Hub) Decide(ctx context.Context, req runtime.ApprovalRequest) (runtime.Decision, error) {
	p := &pending{ch: make(chan runtime.Decision, 1), info: PendingApproval{RunID: string(req.RunID), NodeID: req.NodeID, NodeRunID: req.NodeRunID, Prompt: req.Prompt, Context: append([]events.ApprovalContext{}, req.Context...)}}
	h.mu.Lock()
	h.pending[req.NodeRunID] = p
	old := h.registered
	h.registered = make(chan struct{})
	hook := h.afterRegister
	h.mu.Unlock()
	close(old)
	if hook != nil {
		hook()
	}
	select {
	case d := <-p.ch:
		return d, nil
	case <-ctx.Done():
		h.mu.Lock()
		defer h.mu.Unlock()
		// A decision accepted by Submit before cancellation took effect must
		// not be lost: the client was told it was recorded.
		select {
		case d := <-p.ch:
			return d, nil
		default:
		}
		delete(h.pending, req.NodeRunID)
		return runtime.Decision{}, ctx.Err()
	}
}

// Submit records the first decision for a node run. A repeat returns the
// original decision with already=true and changes nothing. The run ID must
// match, so a stale link can never approve a different run.
func (h *Hub) Submit(runID string, nodeRunID events.NodeRunID, approve bool, comment string) (rec Recorded, already bool, err error) {
	if len(comment) > events.MaxApprovalComment || !utf8.ValidString(comment) {
		return Recorded{}, false, ErrInvalidComment
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if prior, ok := h.decided[nodeRunID]; ok {
		if prior.runID != runID {
			return Recorded{}, false, ErrNotPending
		}
		return prior.rec, true, nil
	}
	p, ok := h.pending[nodeRunID]
	if !ok || p.info.RunID != runID {
		return Recorded{}, false, ErrNotPending
	}
	verdict := events.ApprovalRejected
	if approve {
		verdict = events.ApprovalApproved
	}
	rec = Recorded{Decision: verdict, Comment: comment}
	h.decided[nodeRunID] = decidedRecord{runID: runID, rec: rec}
	delete(h.pending, nodeRunID)
	p.ch <- runtime.Decision{Approve: approve, Comment: comment}
	return rec, false, nil
}
