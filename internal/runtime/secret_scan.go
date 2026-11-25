package runtime

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

// WorkspaceScanner opens an observation window over a node's workspace root
// before the node runs (F3.8 requirement 13, ADR-0029). The runtime owns when
// to scan and what the result means; an adapter owns Git and file access. A
// scanner reports "no evidence" by returning an error that wraps
// secrets.ErrUnobservable; any other error is recorded as a failed observation.
// Neither ever changes the outcome of the node.
type WorkspaceScanner interface {
	// Begin captures the state of root. An empty root means the runner's own
	// working directory; the adapter resolves it. The returned window must not
	// depend on ctx outliving the call.
	Begin(ctx context.Context, root string) (ScanWindow, error)
}

// ScanWindow reports which of the granted values the files changed since Begin
// contain. The report holds paths and secret names, never a value.
type ScanWindow interface {
	Scan(ctx context.Context, targets []secrets.ScanTarget) (secrets.ScanReport, error)
}

// scanTimeout bounds each of Begin and Scan so a huge workspace degrades to
// "unobserved" instead of stalling the node run.
const scanTimeout = 30 * time.Second

// scanSession is the open window of one node run, or why there is none.
type scanSession struct {
	window ScanWindow
	err    error
}

// scanBook holds the open scan sessions by node ID. A node is driven by one
// worker at a time, so the mutex only protects the map itself.
type scanBook struct {
	mu       sync.Mutex
	sessions map[string]scanSession
}

func (b *scanBook) lookup(id string) (scanSession, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	session, ok := b.sessions[id]
	return session, ok
}

func (b *scanBook) store(id string, session scanSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions == nil {
		b.sessions = map[string]scanSession{}
	}
	b.sessions[id] = session
}

func (b *scanBook) take(id string) scanSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	session := b.sessions[id]
	delete(b.sessions, id)
	return session
}

// scanRequest identifies the node run a worker drives.
type scanRequest struct {
	id       string
	node     graph.NodeDefinition
	number   int
	redactor *logRedactor
}

// scanned wraps one worker cycle. A node without a grant runs untouched: no
// port call and no event. A granted node gets its window before the first
// attempt and one scan after the last one, in the worker goroutine so a slow
// scan never blocks the coordinator or sibling nodes. A cancelled run is not
// scanned: nothing is known about the workspace and an event would blur the
// trace. The result is carried on the worker result; the coordinator emits it.
func (m *runMachine) scanned(ctx context.Context, request scanRequest, produce func() completedNode) completedNode {
	targets := m.secretEnvFor(request.id, request.node).Targets()
	if len(targets) == 0 || ctx.Err() != nil {
		return produce()
	}
	if _, open := m.scans.lookup(request.id); !open {
		m.scans.store(request.id, m.beginScan(ctx, scanRoot(request.node)))
	}
	work := produce()
	if ctx.Err() != nil {
		m.scans.take(request.id)
		return work
	}
	if attemptWillRetry(ctx, request, work) {
		return work
	}
	check := m.finishScan(ctx, m.scans.take(request.id), targets, request.redactor)
	work.scan = &check
	return work
}

// scanRoot is the directory a node works in: the Agent workspace (the isolated
// worktree after isolatedNode) or the Shell cwd. Empty means the runner's own.
func scanRoot(node graph.NodeDefinition) string {
	for _, key := range []string{"workspace", "cwd"} {
		if root, ok := node.Config[key].(string); ok {
			return root
		}
	}
	return ""
}

func (m *runMachine) beginScan(ctx context.Context, root string) scanSession {
	if m.scanner == nil {
		return scanSession{err: errScannerNotConfigured}
	}
	ctx, cancel := context.WithTimeout(ctx, scanTimeout)
	defer cancel()
	window, err := m.scanner.Begin(ctx, root)
	if err == nil && window == nil {
		err = errors.New("workspace scanner returned no window")
	}
	return scanSession{window: window, err: err}
}

func (m *runMachine) finishScan(ctx context.Context, session scanSession, targets []secrets.ScanTarget, redactor *logRedactor) events.GateCheckPayload {
	observation := scanObservation{err: session.err}
	if session.err == nil && session.window == nil {
		observation.err = errors.New("no open scan window for this node run")
	}
	if observation.err == nil {
		ctx, cancel := context.WithTimeout(ctx, scanTimeout)
		defer cancel()
		started := m.clock.Now()
		observation.report, observation.err = session.window.Scan(ctx, targets)
		observation.duration = m.clock.Now().Sub(started)
	}
	return secretScanCheck(observation, pathRedactor(redactor))
}

// attemptWillRetry mirrors the rule acceptBatchResults applies to a red,
// valid check below the attempt budget: only then does another attempt follow.
func attemptWillRetry(ctx context.Context, request scanRequest, work completedNode) bool {
	loop := request.node.Loop
	return loop != nil && work.check != nil && !work.checkInvalid &&
		work.check.Verdict == events.CheckRed && request.number < loop.Attempts() && ctx.Err() == nil
}

// recordSecretScans emits each finished node's scan result before anything else
// the coordinator derives from the batch, so the fact precedes
// workspace_integrated and node_finished. Until F3.9 adds a policy mode a red
// result is an audit fact only: it changes neither node nor run outcome. F3.9
// hooks its enforcement decision here (docs/features/F3.8-secrets-references.md).
func (m *runMachine) recordSecretScans(results []completedNode) {
	for _, work := range results {
		if work.scan == nil {
			continue
		}
		m.emitSecretScan(work.id, *work.scan)
	}
}

func (m *runMachine) emitSecretScan(id string, check events.GateCheckPayload) {
	m.appendEvent(events.Event{Type: events.GateCheck, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, Payload: events.CloneGate(check)})
}

// closeUnscannedWindows is the safety net for the one rule the worker and the
// coordinator both apply to decide "another attempt follows" (attemptWillRetry
// mirrors accept.go). If they ever disagree, the worker leaves the window open
// for a retry that never comes. Rather than trust that the two copies stay in
// step, the coordinator, which makes the final decision, closes every window of
// a node it is not retrying and records an unobserved scan (reason
// scan_not_completed). A cancelled run records nothing, like the worker.
func (m *runMachine) closeUnscannedWindows(ctx context.Context, results []completedNode, retry []string) {
	for _, work := range results {
		if slices.Contains(retry, work.id) {
			continue
		}
		if _, open := m.scans.lookup(work.id); !open {
			continue
		}
		m.scans.take(work.id)
		if ctx.Err() == nil {
			m.emitSecretScan(work.id, unobservedCheck(scanObservation{}, "scan_not_completed"))
		}
	}
}
