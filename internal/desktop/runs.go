package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"agentplane/internal/events"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

const maxOwnedRunKeys = 64

// RunStartRequest is the validated private Start payload. PreflightToken
// authorizes a new launch but is excluded from accepted-key idempotency.
type RunStartRequest struct {
	IdempotencyKey string
	PreflightToken string
	PayloadDigest  string
	DocumentID     string
	Revision       uint64
	WorkspaceID    string
	Inputs         map[string]graph.Value
	MaxConcurrency int
}

// PreparedRun is a detached, rechecked launch snapshot. A preparer must not
// return mutable graph or input state shared with editor buffers or callers.
type PreparedRun struct {
	Definition   *graph.Definition
	Inputs       map[string]graph.Value
	Executor     runtime.Executor
	Options      runtime.Options
	GraphVersion string
}

type StartPreparer func(context.Context, RunStartRequest) (PreparedRun, *ServiceError)
type RunRunner func(context.Context, *graph.Definition, map[string]graph.Value, runtime.Executor, runtime.Options) (runtime.RunResult, error)

type RunStartResult struct {
	RunID          events.RunID     `json:"run_id"`
	State          runtime.RunState `json:"state"`
	GraphVersion   string           `json:"graph_version"`
	TraceWatermark uint64           `json:"trace_watermark"`
}

type RunCancelResult struct {
	RunID events.RunID     `json:"run_id"`
	State runtime.RunState `json:"state"`
}

type RunNodeSummary struct {
	NodeID            string              `json:"node_id"`
	NodeRunID         events.NodeRunID    `json:"node_run_id"`
	State             runtime.NodeState   `json:"state"`
	AcceptedAttemptID events.AttemptID    `json:"accepted_attempt_id,omitempty"`
	FailureKind       runtime.FailureKind `json:"failure_kind,omitempty"`
}

type RunSnapshot struct {
	RunID          events.RunID        `json:"run_id"`
	GraphID        string              `json:"graph_id"`
	GraphVersion   string              `json:"graph_version"`
	State          runtime.RunState    `json:"state"`
	TraceWatermark uint64              `json:"trace_watermark"`
	DeliveryStats  RunDeliveryStats    `json:"delivery_stats"`
	Finished       bool                `json:"finished"`
	Nodes          []RunNodeSummary    `json:"nodes,omitempty"`
	FailureKind    runtime.FailureKind `json:"failure_kind,omitempty"`
}

type RunDeliveryStats struct {
	RetainedOmittedEvents uint64 `json:"retained_omitted_events"`
	RetainedOmittedBytes  uint64 `json:"retained_omitted_bytes"`
	LiveOmittedEvents     uint64 `json:"live_omitted_events"`
	LiveOmittedBytes      uint64 `json:"live_omitted_bytes"`
	LiveStopped           bool   `json:"live_stopped"`
}

type ownedRun struct {
	key, fingerprint string
	ready, done      chan struct{}
	readyClosed      bool
	startErr         *ServiceError
	cancel           context.CancelFunc
	runID            events.RunID
	reader           events.TraceReader
	graphID          string
	graphVersion     string
	state            runtime.RunState
	finished         bool
	nodes            []RunNodeSummary
	failureKind      runtime.FailureKind
	watermark        uint64
}

// RunService owns one local run at a time. Accepted identities remain for the
// service lifetime; only the active and most recent completed trace are held.
type RunService struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	prepare  StartPreparer
	runner   RunRunner
	active   *ownedRun
	accepted map[string]*ownedRun
	byID     map[events.RunID]*ownedRun
	lastDone *ownedRun
	closed   bool
}

func NewRunService(lifetime context.Context, prepare StartPreparer, runner RunRunner) *RunService {
	if lifetime == nil {
		lifetime = context.Background()
	}
	ctx, cancel := context.WithCancel(lifetime)
	if runner == nil {
		runner = runtime.Run
	}
	return &RunService{ctx: ctx, cancel: cancel, prepare: prepare, runner: runner,
		accepted: make(map[string]*ownedRun), byID: make(map[events.RunID]*ownedRun)}
}

// Start joins an in-flight identical key, returns an accepted identity, or
// reserves the sole active slot. Request cancellation stops waiting only; the
// service lifetime owns preparation and execution across renderer reloads.
func (s *RunService) Start(requestCtx context.Context, request RunStartRequest) (RunStartResult, *ServiceError) {
	if s == nil || requestCtx == nil {
		return RunStartResult{}, protocolError("unavailable", "Run service is unavailable")
	}
	fingerprint, err := startFingerprint(request)
	if err != nil {
		return RunStartResult{}, protocolError("invalid_request", "Invalid run start payload")
	}
	if requestCtx.Err() != nil {
		return RunStartResult{}, protocolError("unavailable", "Start request was interrupted")
	}
	s.mu.Lock()
	if requestCtx.Err() != nil {
		s.mu.Unlock()
		return RunStartResult{}, protocolError("unavailable", "Start request was interrupted")
	}
	if s.closed || s.ctx.Err() != nil || s.prepare == nil {
		s.mu.Unlock()
		return RunStartResult{}, protocolError("unavailable", "Run service is unavailable")
	}
	if accepted := s.accepted[request.IdempotencyKey]; accepted != nil {
		if accepted.fingerprint != fingerprint {
			s.mu.Unlock()
			return RunStartResult{}, protocolError("conflict", "Start key is bound to a different payload")
		}
		result, serviceErr := startResultLocked(accepted)
		s.mu.Unlock()
		return result, serviceErr
	}
	if s.active != nil {
		if s.active.key != request.IdempotencyKey {
			s.mu.Unlock()
			return RunStartResult{}, protocolError("busy", "A local run is already active")
		}
		if s.active.fingerprint != fingerprint {
			s.mu.Unlock()
			return RunStartResult{}, protocolError("conflict", "Start key is bound to a different payload")
		}
		pending := s.active
		s.mu.Unlock()
		return waitForStart(requestCtx, s, pending)
	}
	if len(s.accepted) >= maxOwnedRunKeys {
		s.mu.Unlock()
		return RunStartResult{}, protocolError("unavailable", "Local run identity capacity is exhausted")
	}
	record := &ownedRun{key: request.IdempotencyKey, fingerprint: fingerprint,
		ready: make(chan struct{}), done: make(chan struct{})}
	s.active = record
	s.mu.Unlock()
	go s.prepareAndRun(record, request)
	return waitForStart(requestCtx, s, record)
}

func waitForStart(ctx context.Context, s *RunService, record *ownedRun) (RunStartResult, *ServiceError) {
	select {
	case <-record.ready:
	case <-ctx.Done():
		return RunStartResult{}, protocolError("unavailable", "Start response was interrupted; retry with the same key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return startResultLocked(record)
}

func startResultLocked(record *ownedRun) (RunStartResult, *ServiceError) {
	if record.startErr != nil {
		return RunStartResult{}, record.startErr
	}
	if record.runID == "" {
		return RunStartResult{}, protocolError("unavailable", "Run has no published identity")
	}
	watermark := record.watermark
	if record.reader != nil {
		page, err := record.reader.Page(0, 1)
		if err != nil {
			return RunStartResult{}, protocolError("unavailable", "Run trace is unavailable")
		}
		watermark = page.Watermark
		record.watermark = watermark
	}
	return RunStartResult{RunID: record.runID, State: record.state,
		GraphVersion: record.graphVersion, TraceWatermark: watermark}, nil
}

func (s *RunService) prepareAndRun(record *ownedRun, request RunStartRequest) {
	prepared, serviceErr := s.prepare(s.ctx, request)
	if serviceErr == nil && (prepared.Definition == nil || prepared.Executor == nil || prepared.GraphVersion == "" || prepared.Options.OnRunReady != nil) {
		serviceErr = protocolError("unavailable", "Prepared run is unavailable")
	}
	if serviceErr == nil && s.ctx.Err() != nil {
		serviceErr = protocolError("unavailable", "Run service is closing")
	}
	if serviceErr != nil {
		s.finishBeforeReady(record, serviceErr)
		return
	}
	runCtx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	record.cancel = cancel
	s.mu.Unlock()
	options := prepared.Options
	options.OnRunReady = func(id events.RunID, reader events.TraceReader) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if record.readyClosed {
			return
		}
		if id == "" || reader == nil || s.byID[id] != nil {
			record.startErr = protocolError("unavailable", "Runtime did not publish a valid run identity")
			cancel()
		} else {
			record.runID, record.reader = id, reader
			record.graphID, record.graphVersion = prepared.Definition.ID, prepared.GraphVersion
			record.state = runtime.RunRunning
			s.accepted[record.key] = record
			s.byID[id] = record
		}
		close(record.ready)
		record.readyClosed = true
	}
	result, err := s.runner(runCtx, prepared.Definition, prepared.Inputs, prepared.Executor, options)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !record.readyClosed {
		// runtime.Run promises an error before OnRunReady means no run started.
		// No ID or accepted key is invented for this failure.
		record.startErr = protocolError("unavailable", "Runtime could not start the run")
		close(record.ready)
		record.readyClosed = true
	} else if record.runID != "" {
		if err == nil && result.RunID == record.runID && result.GraphVersion == record.graphVersion && terminalRunState(result.State) {
			record.state = result.State
			record.finished = true
			record.nodes = safeNodeSummaries(result.Nodes)
			if result.Failure != nil {
				record.failureKind = result.Failure.Kind
			}
		} else {
			// An unexpected post-ready runtime error has no terminal fact. Make
			// the uncertainty explicit instead of inventing a successful run.
			record.state = "unknown"
		}
		if s.lastDone != nil && s.lastDone != record {
			s.lastDone.reader = nil
			s.lastDone.nodes = nil
		}
		s.lastDone = record
	}
	if s.active == record {
		s.active = nil
	}
	close(record.done)
}

func (s *RunService) finishBeforeReady(record *ownedRun, serviceErr *ServiceError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.startErr = serviceErr
	if !record.readyClosed {
		close(record.ready)
		record.readyClosed = true
	}
	if s.active == record {
		s.active = nil
	}
	close(record.done)
}

func terminalRunState(state runtime.RunState) bool {
	return state == runtime.RunSucceeded || state == runtime.RunFailed || state == runtime.RunCancelled
}

func safeNodeSummaries(nodes []runtime.NodeResult) []RunNodeSummary {
	result := make([]RunNodeSummary, 0, len(nodes))
	for _, node := range nodes {
		item := RunNodeSummary{NodeID: node.NodeID, NodeRunID: node.NodeRunID,
			State: node.State, AcceptedAttemptID: node.AcceptedAttemptID}
		if node.Failure != nil {
			item.FailureKind = node.Failure.Kind
		}
		result = append(result, item)
	}
	return result
}

func (s *RunService) Snapshot(runID events.RunID) (RunSnapshot, *ServiceError) {
	if s == nil || runID == "" {
		return RunSnapshot{}, protocolError("not_found", "Run identity was not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.byID[runID]
	if record == nil {
		return RunSnapshot{}, protocolError("not_found", "Run identity was not found")
	}
	if record.reader == nil {
		return RunSnapshot{}, protocolError("unavailable", "Older in-memory run trace is unavailable")
	}
	return runSnapshotLocked(record)
}

func runSnapshotLocked(record *ownedRun) (RunSnapshot, *ServiceError) {
	snapshot := RunSnapshot{RunID: record.runID, GraphID: record.graphID,
		GraphVersion: record.graphVersion, State: record.state, Finished: record.finished,
		FailureKind: record.failureKind, TraceWatermark: record.watermark}
	if record.reader != nil {
		page, err := record.reader.Page(0, 1)
		if err != nil {
			return RunSnapshot{}, protocolError("unavailable", "Run trace is unavailable")
		}
		snapshot.TraceWatermark = page.Watermark
		snapshot.DeliveryStats = RunDeliveryStats{
			RetainedOmittedEvents: page.Stats.RetainedOmittedEvents,
			RetainedOmittedBytes:  page.Stats.RetainedOmittedBytes,
			LiveOmittedEvents:     page.Stats.LiveOmittedEvents,
			LiveOmittedBytes:      page.Stats.LiveOmittedBytes,
			LiveStopped:           page.Stats.LiveStopped,
		}
		record.watermark = page.Watermark
	}
	if record.finished {
		snapshot.Nodes = append([]RunNodeSummary(nil), record.nodes...)
	}
	return snapshot, nil
}

// Cancel targets one exact RunID. It joins runtime.Run before reporting its
// result; request cancellation cannot turn incomplete cleanup into success.
func (s *RunService) Cancel(_ context.Context, runID events.RunID) (RunCancelResult, *ServiceError) {
	if s == nil || runID == "" {
		return RunCancelResult{}, protocolError("not_found", "Run identity was not found")
	}
	s.mu.Lock()
	record := s.byID[runID]
	if record == nil {
		s.mu.Unlock()
		return RunCancelResult{}, protocolError("not_found", "Run identity was not found")
	}
	if record.cancel != nil && !record.finished {
		record.cancel()
	}
	done := record.done
	s.mu.Unlock()
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	return RunCancelResult{RunID: record.runID, State: record.state}, nil
}

// Close cancels the service lifetime and joins the active reservation/run.
// It is safe to call more than once, including after protocol EOF.
func (s *RunService) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.cancel()
	var done <-chan struct{}
	if s.active != nil {
		done = s.active.done
		if s.active.cancel != nil {
			s.active.cancel()
		}
	}
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

func startFingerprint(request RunStartRequest) (string, error) {
	if !validIdempotencyKey(request.IdempotencyKey) || !validPreflightToken(request.PreflightToken) ||
		!validDigest(request.PayloadDigest) || !validSaveID(request.DocumentID) || request.Revision == 0 ||
		!validSaveID(request.WorkspaceID) || request.MaxConcurrency < 1 || request.MaxConcurrency > 32 {
		return "", errors.New("invalid start selection")
	}
	inputs, err := encodeTypedInputsExact(request.Inputs)
	if err != nil {
		return "", err
	}
	var payload []byte
	for _, field := range [][]byte{[]byte("agentplane-start-v1"), []byte(request.PayloadDigest),
		[]byte(request.DocumentID), []byte(fmt.Sprint(request.Revision)),
		[]byte(request.WorkspaceID), []byte(fmt.Sprint(request.MaxConcurrency)), inputs} {
		if uint64(len(field)) > uint64(^uint32(0)) {
			return "", errors.New("start field exceeds bound")
		}
		payload = binary.BigEndian.AppendUint32(payload, uint32(len(field)))
		payload = append(payload, field...)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func validIdempotencyKey(key string) bool {
	if len(key) != 36 {
		return false
	}
	for i := range key {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if key[i] != '-' {
				return false
			}
			continue
		}
		if key[i] < '0' || key[i] > '9' && (key[i] < 'a' || key[i] > 'f') {
			return false
		}
	}
	return key[14] >= '1' && key[14] <= '8' && key[19] >= '8' && key[19] <= 'b'
}

func validPreflightToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for i := range token {
		if token[i] < '0' || token[i] > '9' && (token[i] < 'a' || token[i] > 'f') {
			return false
		}
	}
	return true
}
