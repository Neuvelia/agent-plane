package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/history"
)

// History handlers expose only typed, bounded safe projections owned by the
// Go store. They never return database or managed-file paths.
type historyHandlerSet struct {
	store    *history.Store
	runs     *RunService
	registry *Registry
}

func NewHistoryHandlers(store *history.Store, runs *RunService, registry *Registry) map[string]Handler {
	h := historyHandlerSet{store: store, runs: runs, registry: registry}
	return map[string]Handler{
		"history.list":          h.list,
		"history.get":           h.get,
		"history.outcomes":      h.outcomes,
		"history.events":        h.events,
		"history.evidence":      h.evidence,
		"history.evidence.list": h.evidenceList,
		"history.pin":           h.pin,
		"history.prune":         h.prune,
	}
}

func (h historyHandlerSet) evidenceList(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "run_id", "attempt_id", "cursor", "limit")
	if serviceErr != nil {
		return nil, serviceErr
	}
	id, ok := historyRunID(fields)
	var attemptID, cursor string
	var limit int
	if !ok || requiredField(fields, "limit", &limit) != nil || limit < 1 || limit > 256 ||
		!optionalHistoryField(fields, "attempt_id", &attemptID) || (attemptID != "" && !validSaveID(attemptID)) ||
		!optionalHistoryField(fields, "cursor", &cursor) || len(cursor) > 4096 {
		return nil, protocolError("invalid_request", "Invalid historical evidence selection")
	}
	if h.runs != nil {
		page, found, err := h.runs.ActiveEvidenceDescriptors(events.RunID(id), attemptID, cursor, limit)
		if err != nil {
			return nil, err
		}
		if found {
			return page, nil
		}
	}
	page, err := h.store.EvidenceDescriptors(ctx, id, attemptID, cursor, limit)
	if err != nil {
		return nil, historyError(err)
	}
	return page, nil
}

func historyError(err error) *ServiceError {
	switch {
	case errors.Is(err, history.ErrNotFound):
		return protocolError("not_found", "Historical run or evidence was not found")
	case errors.Is(err, history.ErrStaleCursor):
		return protocolError("stale_cursor", "History changed; restart pagination")
	case errors.Is(err, history.ErrConflict):
		return protocolError("conflict", "History changed or conflicts with this request")
	case errors.Is(err, history.ErrPageTooLarge):
		return protocolError("page_too_large", "History page is too large; request a smaller limit")
	case errors.Is(err, history.ErrInvalidEvidenceOffset):
		return protocolError("invalid_request", "Evidence offset exceeds content length")
	case errors.Is(err, history.ErrNewerSchema):
		return protocolError("history_version_unsupported", "Local history uses a newer schema; update AgentPlane")
	default:
		return protocolError("history_unavailable", "Local run history is unavailable")
	}
}

func (h historyHandlerSet) available() *ServiceError {
	if h.store == nil {
		if h.runs != nil && errors.Is(h.runs.historyOpenErr, history.ErrNewerSchema) {
			return protocolError("history_version_unsupported", "Local history uses a newer schema; update AgentPlane")
		}
		return protocolError("history_unavailable", "Local run history is unavailable")
	}
	return nil
}

func optionalHistoryField[T any](fields map[string]json.RawMessage, name string, out *T) bool {
	raw, ok := fields[name]
	if !ok {
		return true
	}
	return len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, out) == nil
}

func historyRunID(fields map[string]json.RawMessage) (string, bool) {
	var id string
	if requiredField(fields, "run_id", &id) != nil || !validSaveID(id) {
		return "", false
	}
	return id, true
}

func safeHistoryDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type historyRunSummaryDTO struct {
	RunID              string               `json:"run_id"`
	ProjectID          string               `json:"project_id"`
	GraphID            string               `json:"graph_id"`
	StartedAt          time.Time            `json:"started_at"`
	EndedAt            *time.Time           `json:"ended_at,omitempty"`
	StorageState       history.StorageState `json:"storage_state"`
	RuntimeState       string               `json:"runtime_state,omitempty"`
	Pinned             bool                 `json:"pinned"`
	Revision           uint64               `json:"revision"`
	Completeness       json.RawMessage      `json:"completeness"`
	CompletenessDigest string               `json:"completeness_digest"`
}
type historyRunDetailDTO struct {
	RunID        string               `json:"run_id"`
	ProjectID    string               `json:"project_id"`
	WorkspaceID  string               `json:"workspace_id"`
	DocumentID   string               `json:"document_id"`
	GraphID      string               `json:"graph_id"`
	GraphHash    string               `json:"graph_hash"`
	StartedAt    time.Time            `json:"started_at"`
	EndedAt      *time.Time           `json:"ended_at,omitempty"`
	StorageState history.StorageState `json:"storage_state"`
	RuntimeState string               `json:"runtime_state,omitempty"`
	Pinned       bool                 `json:"pinned"`
	Revision     uint64               `json:"revision"`
}

func (h historyHandlerSet) list(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "project_id", "graph_id", "storage_state", "started_from", "started_before", "cursor", "limit")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var query history.ListQuery
	if requiredField(fields, "limit", &query.Limit) != nil || query.Limit < 1 || query.Limit > 256 ||
		!optionalHistoryField(fields, "project_id", &query.ProjectID) || !optionalHistoryField(fields, "graph_id", &query.GraphID) ||
		!optionalHistoryField(fields, "storage_state", &query.StorageState) || !optionalHistoryField(fields, "cursor", &query.Cursor) || len(query.Cursor) > 4096 ||
		(query.ProjectID != "" && !validSaveID(query.ProjectID)) || (query.GraphID != "" && !validIdentifier(query.GraphID)) ||
		(query.StorageState != "" && query.StorageState != history.Active && query.StorageState != history.Complete && query.StorageState != history.Interrupted) {
		return nil, protocolError("invalid_request", "Invalid history list selection")
	}
	for _, selection := range []struct {
		name   string
		target **time.Time
	}{{"started_from", &query.StartedFrom}, {"started_before", &query.StartedBefore}} {
		if value, ok := fields[selection.name]; ok {
			var text string
			if json.Unmarshal(value, &text) != nil || text == "" {
				return nil, protocolError("invalid_request", "Invalid history time filter")
			}
			parsed, err := time.Parse(time.RFC3339Nano, text)
			if err != nil {
				return nil, protocolError("invalid_request", "Invalid history time filter")
			}
			*selection.target = &parsed
		}
	}
	page, err := h.store.List(ctx, query)
	if err != nil {
		return nil, historyError(err)
	}
	runs := make([]historyRunSummaryDTO, 0, len(page.Runs))
	for _, item := range page.Runs {
		runs = append(runs, historyRunSummaryDTO{RunID: item.RunID, ProjectID: item.ProjectID, GraphID: item.GraphID,
			StartedAt: item.StartedAt, EndedAt: item.EndedAt, StorageState: item.StorageState, RuntimeState: item.RuntimeState,
			Pinned: item.Pinned, Revision: item.Revision, Completeness: item.Completeness.Bytes(), CompletenessDigest: safeHistoryDigest(item.Completeness.Bytes())})
	}
	return struct {
		Runs       []historyRunSummaryDTO `json:"runs"`
		NextCursor string                 `json:"next_cursor,omitempty"`
		Generation uint64                 `json:"generation"`
	}{runs, page.NextCursor, page.Generation}, nil
}

func (h historyHandlerSet) get(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "run_id")
	if serviceErr != nil {
		return nil, serviceErr
	}
	id, ok := historyRunID(fields)
	if !ok {
		return nil, protocolError("invalid_request", "Invalid historical run identity")
	}
	item, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, historyError(err)
	}
	var result json.RawMessage
	var resultDigest string
	if item.Result != nil {
		result = item.Result.Bytes()
		resultDigest = safeHistoryDigest(result)
	}
	graph, launch, completeness := item.Graph.Bytes(), item.Launch.Bytes(), item.Completeness.Bytes()
	return struct {
		Run                historyRunDetailDTO `json:"run"`
		Graph              json.RawMessage     `json:"graph"`
		GraphDigest        string              `json:"graph_digest"`
		Launch             json.RawMessage     `json:"launch"`
		LaunchDigest       string              `json:"launch_digest"`
		Result             json.RawMessage     `json:"result,omitempty"`
		ResultDigest       string              `json:"result_digest,omitempty"`
		Completeness       json.RawMessage     `json:"completeness"`
		CompletenessDigest string              `json:"completeness_digest"`
	}{Run: historyRunDetailDTO{RunID: item.RunID, ProjectID: item.ProjectID, WorkspaceID: item.WorkspaceID, DocumentID: item.DocumentID, GraphID: item.GraphID, GraphHash: item.GraphHash,
		StartedAt: item.StartedAt, EndedAt: item.EndedAt, StorageState: item.StorageState, RuntimeState: item.RuntimeState, Pinned: item.Pinned, Revision: item.Revision},
		Graph: graph, GraphDigest: safeHistoryDigest(graph), Launch: launch, LaunchDigest: safeHistoryDigest(launch), Result: result, ResultDigest: resultDigest,
		Completeness: completeness, CompletenessDigest: safeHistoryDigest(completeness)}, nil
}

type historyNodeDTO struct {
	NodeRunID  string          `json:"node_run_id"`
	NodeID     string          `json:"node_id"`
	Status     string          `json:"status"`
	Projection json.RawMessage `json:"projection"`
	Digest     string          `json:"digest"`
}
type historyAttemptDTO struct {
	AttemptID  string          `json:"attempt_id"`
	NodeRunID  string          `json:"node_run_id"`
	Status     string          `json:"status"`
	Projection json.RawMessage `json:"projection"`
	Digest     string          `json:"digest"`
}

func (h historyHandlerSet) outcomes(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "run_id", "limit", "node_cursor", "attempt_cursor")
	if serviceErr != nil {
		return nil, serviceErr
	}
	id, ok := historyRunID(fields)
	var limit int
	var nodeCursor, attemptCursor string
	if !ok || requiredField(fields, "limit", &limit) != nil || limit < 1 || limit > 256 || !optionalHistoryField(fields, "node_cursor", &nodeCursor) || !optionalHistoryField(fields, "attempt_cursor", &attemptCursor) || len(nodeCursor) > 4096 || len(attemptCursor) > 4096 {
		return nil, protocolError("invalid_request", "Invalid historical outcome page")
	}
	page, err := h.store.Outcomes(ctx, id, limit, nodeCursor, attemptCursor)
	if err != nil {
		return nil, historyError(err)
	}
	nodes := make([]historyNodeDTO, 0, len(page.Nodes))
	for _, item := range page.Nodes {
		b := item.Projection.Bytes()
		nodes = append(nodes, historyNodeDTO{item.NodeRunID, item.NodeID, item.Status, b, safeHistoryDigest(b)})
	}
	attempts := make([]historyAttemptDTO, 0, len(page.Attempts))
	for _, item := range page.Attempts {
		b := item.Projection.Bytes()
		attempts = append(attempts, historyAttemptDTO{item.AttemptID, item.NodeRunID, item.Status, b, safeHistoryDigest(b)})
	}
	return struct {
		Nodes             []historyNodeDTO    `json:"nodes"`
		Attempts          []historyAttemptDTO `json:"attempts"`
		NextNodeCursor    string              `json:"next_node_cursor,omitempty"`
		NextAttemptCursor string              `json:"next_attempt_cursor,omitempty"`
	}{nodes, attempts, page.NextNodeCursor, page.NextAttemptCursor}, nil
}

type historyEventDTO struct {
	Sequence   uint64          `json:"sequence"`
	Projection json.RawMessage `json:"projection"`
	Digest     string          `json:"digest"`
}
type historyOmissionDTO struct {
	From   uint64 `json:"from"`
	To     uint64 `json:"to"`
	Reason string `json:"reason"`
}

func (h historyHandlerSet) events(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "run_id", "after_sequence", "limit")
	if serviceErr != nil {
		return nil, serviceErr
	}
	id, ok := historyRunID(fields)
	var after uint64
	var limit int
	if !ok || requiredField(fields, "after_sequence", &after) != nil || requiredField(fields, "limit", &limit) != nil || limit < 1 || limit > 256 {
		return nil, protocolError("invalid_request", "Invalid historical event page")
	}
	page, err := h.store.Events(ctx, id, after, limit)
	if err != nil {
		return nil, historyError(err)
	}
	events := make([]historyEventDTO, 0, len(page.Events))
	for _, item := range page.Events {
		b := item.Projection.Bytes()
		events = append(events, historyEventDTO{item.Sequence, b, safeHistoryDigest(b)})
	}
	omissions := make([]historyOmissionDTO, 0, len(page.Omissions))
	for _, item := range page.Omissions {
		omissions = append(omissions, historyOmissionDTO{item.From, item.To, item.Reason})
	}
	return struct {
		Events             []historyEventDTO    `json:"events"`
		Omissions          []historyOmissionDTO `json:"omissions"`
		NextSequence       uint64               `json:"next_sequence"`
		Watermark          uint64               `json:"watermark"`
		Finished           bool                 `json:"finished"`
		Complete           bool                 `json:"complete"`
		CompletenessDigest string               `json:"completeness_digest"`
	}{events, omissions, page.NextSequence, page.Watermark, page.Finished, page.Complete, page.CompletenessDigest}, nil
}

func (h historyHandlerSet) evidence(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "evidence_id", "offset", "limit")
	if serviceErr != nil {
		return nil, serviceErr
	}
	var id string
	var offset int64
	var limit int
	if requiredField(fields, "evidence_id", &id) != nil || !validIdentifier(id) || requiredField(fields, "offset", &offset) != nil || offset < 0 || requiredField(fields, "limit", &limit) != nil || limit < 1 || limit > 1<<20 {
		return nil, protocolError("invalid_request", "Invalid historical evidence chunk")
	}
	if h.runs != nil {
		if unavailable, found := h.runs.ActiveEvidenceUnavailable(id); found {
			return historyEvidenceChunkDTO(unavailable), nil
		}
	}
	chunk, err := h.store.Evidence(ctx, id, offset, limit)
	if err != nil {
		return nil, historyError(err)
	}
	return historyEvidenceChunkDTO(chunk), nil
}

func historyEvidenceChunkDTO(chunk history.EvidenceChunk) any {
	return struct {
		Availability   string `json:"availability"`
		OmissionReason string `json:"omission_reason,omitempty"`
		TotalLength    int64  `json:"total_length"`
		Digest         string `json:"digest,omitempty"`
		BytesBase64    string `json:"bytes_base64"`
		NextOffset     int64  `json:"next_offset"`
		End            bool   `json:"end"`
	}{chunk.Availability, chunk.OmissionReason, chunk.TotalLength, chunk.Digest, base64.StdEncoding.EncodeToString(chunk.Data), chunk.NextOffset, chunk.End}
}

func (h historyHandlerSet) pin(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	fields, serviceErr := strictParams(raw, "run_id", "expected_revision", "pinned")
	if serviceErr != nil {
		return nil, serviceErr
	}
	id, ok := historyRunID(fields)
	var revision uint64
	var pinned bool
	if !ok || requiredField(fields, "expected_revision", &revision) != nil || requiredField(fields, "pinned", &pinned) != nil {
		return nil, protocolError("invalid_request", "Invalid history pin request")
	}
	next, err := h.store.Pin(ctx, id, revision, pinned)
	if err != nil {
		return nil, historyError(err)
	}
	return struct {
		Revision uint64 `json:"revision"`
		Pinned   bool   `json:"pinned"`
	}{next, pinned}, nil
}

func (h historyHandlerSet) prune(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if err := h.available(); err != nil {
		return nil, err
	}
	if h.registry == nil {
		return nil, protocolError("history_unavailable", "Local history settings are unavailable")
	}
	fields, serviceErr := strictParams(raw, "retention_days", "keep_all")
	if serviceErr != nil {
		return nil, serviceErr
	}
	settings, err := h.registry.Settings()
	if err != nil {
		return nil, registryError(err)
	}
	options := history.PruneOptions{Now: time.Now().UTC(), KeepAll: settings.KeepAllHistory}
	if settings.HistoryRetentionDays != nil {
		options.RetentionDays = *settings.HistoryRetentionDays
	}
	if _, ok := fields["keep_all"]; ok {
		var keep bool
		if requiredField(fields, "keep_all", &keep) != nil || !keep || fields["retention_days"] != nil {
			return nil, protocolError("invalid_request", "Invalid history retention override")
		}
		options.KeepAll = true
		options.RetentionDays = 0
	}
	if _, ok := fields["retention_days"]; ok {
		var days int
		if requiredField(fields, "retention_days", &days) != nil || days < 1 || days > 3650 || fields["keep_all"] != nil {
			return nil, protocolError("invalid_request", "Invalid history retention override")
		}
		options.KeepAll = false
		options.RetentionDays = days
	}
	result, err := h.store.Prune(ctx, options)
	if err != nil {
		return nil, historyError(err)
	}
	return struct {
		RemovedRuns        int `json:"removed_runs"`
		RemovedFiles       int `json:"removed_files"`
		SkippedActive      int `json:"skipped_active"`
		SkippedInterrupted int `json:"skipped_interrupted"`
		SkippedPinned      int `json:"skipped_pinned"`
		SkippedPending     int `json:"skipped_pending"`
	}{result.RemovedRuns, result.RemovedFiles, result.SkippedActive, result.SkippedInterrupted, result.SkippedPinned, result.SkippedPending}, nil
}
