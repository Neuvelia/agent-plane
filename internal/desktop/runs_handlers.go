package desktop

import (
	"context"
	"encoding/json"

	"agentplane/internal/events"
)

type runHandlerSet struct{ runs *RunService }

func NewRunHandlers(runs *RunService) map[string]Handler {
	set := runHandlerSet{runs: runs}
	return map[string]Handler{
		"runs.start":    set.start,
		"runs.cancel":   set.cancel,
		"runs.snapshot": set.snapshot,
		"runs.events":   set.events,
	}
}

func (h runHandlerSet) start(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if h.runs == nil {
		return nil, protocolError("unavailable", "Run service is unavailable")
	}
	fields, err := strictParams(raw, "idempotency_key", "preflight_token", "payload_digest", "document_id", "revision", "workspace_id", "inputs", "max_concurrency")
	if err != nil {
		return nil, err
	}
	var request RunStartRequest
	if requiredField(fields, "idempotency_key", &request.IdempotencyKey) != nil || !validIdempotencyKey(request.IdempotencyKey) ||
		requiredField(fields, "preflight_token", &request.PreflightToken) != nil || !validPreflightToken(request.PreflightToken) ||
		requiredField(fields, "payload_digest", &request.PayloadDigest) != nil || !validDigest(request.PayloadDigest) ||
		requiredField(fields, "document_id", &request.DocumentID) != nil || !validSaveID(request.DocumentID) ||
		requiredField(fields, "revision", &request.Revision) != nil || request.Revision == 0 ||
		requiredField(fields, "workspace_id", &request.WorkspaceID) != nil || !validSaveID(request.WorkspaceID) ||
		requiredField(fields, "max_concurrency", &request.MaxConcurrency) != nil ||
		request.MaxConcurrency < 1 || request.MaxConcurrency > 32 {
		return nil, protocolError("invalid_request", "Invalid run start selection")
	}
	inputs, decodeErr := decodeTypedInputs(fields["inputs"])
	if decodeErr != nil {
		return nil, protocolError("invalid_request", "Invalid typed graph inputs")
	}
	request.Inputs = inputs
	result, serviceErr := h.runs.Start(ctx, request)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return result, nil
}

func (h runHandlerSet) cancel(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if h.runs == nil {
		return nil, protocolError("unavailable", "Run service is unavailable")
	}
	runID, err := decodeRunID(raw)
	if err != nil {
		return nil, err
	}
	result, serviceErr := h.runs.Cancel(ctx, runID)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return result, nil
}

func (h runHandlerSet) snapshot(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if h.runs == nil {
		return nil, protocolError("unavailable", "Run service is unavailable")
	}
	runID, err := decodeRunID(raw)
	if err != nil {
		return nil, err
	}
	result, serviceErr := h.runs.Snapshot(runID)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return result, nil
}

func (h runHandlerSet) events(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if h.runs == nil {
		return nil, protocolError("unavailable", "Run service is unavailable")
	}
	fields, err := strictParams(raw, "run_id", "after_sequence", "limit")
	if err != nil {
		return nil, err
	}
	var id string
	var after uint64
	var limit int
	if requiredField(fields, "run_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "after_sequence", &after) != nil ||
		requiredField(fields, "limit", &limit) != nil || limit < 1 || limit > events.MaxTracePageItems {
		return nil, protocolError("invalid_request", "Invalid event page selection")
	}
	result, serviceErr := h.runs.Events(events.RunID(id), after, limit)
	if serviceErr != nil {
		return nil, serviceErr
	}
	return result, nil
}

func decodeRunID(raw json.RawMessage) (events.RunID, *ServiceError) {
	fields, err := strictParams(raw, "run_id")
	if err != nil {
		return "", err
	}
	var id string
	if requiredField(fields, "run_id", &id) != nil || !validSaveID(id) {
		return "", protocolError("invalid_request", "Invalid run identity")
	}
	return events.RunID(id), nil
}
