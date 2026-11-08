package desktop

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
)

type documentDTO struct {
	DocumentID      string           `json:"document_id"`
	ProjectID       string           `json:"project_id"`
	RelativePath    string           `json:"relative_path"`
	TextBase64      string           `json:"text_base64"`
	Revision        uint64           `json:"revision"`
	DiskDigest      string           `json:"disk_digest"`
	Validity        DocumentValidity `json:"validity"`
	SemanticVersion string           `json:"semantic_version,omitempty"`
	DiagnosticSummary
}

func toDocumentDTO(doc Document) documentDTO {
	return documentDTO{DocumentID: doc.DocumentID, ProjectID: doc.ProjectID,
		RelativePath: doc.RelativePath, TextBase64: base64.StdEncoding.EncodeToString([]byte(doc.Text)), Revision: doc.Revision,
		DiskDigest: doc.DiskDigest, Validity: doc.Validity,
		SemanticVersion: doc.SemanticVersion, DiagnosticSummary: summarizeDiagnostics(doc.Diagnostics)}
}

type documentHandlerSet struct {
	documents *DocumentStore
	preflight *PreflightService
	recovery  *ServiceError
}

// NewDocumentHandlers recovers pending document exchanges before exposing any
// document or preflight method. An ambiguous recovery keeps all such methods
// unavailable until the user resolves the local state and restarts the service.
func NewDocumentHandlers(store *DocumentStore, preflight *PreflightService) map[string]Handler {
	set := &documentHandlerSet{documents: store, preflight: preflight}
	if store == nil || preflight == nil {
		set.recovery = protocolError("unavailable", "Graph document service is unavailable")
	} else if report, err := store.Recover(); err != nil || len(report.Conflicts) > 0 {
		set.recovery = protocolError("unavailable", "Graph save recovery requires local resolution and service restart")
	}
	return map[string]Handler{
		"documents.open":         set.documentsOpen,
		"documents.create":       set.documentsCreate,
		"documents.reload":       set.documentsReload,
		"documents.inspect":      set.documentsInspect,
		"documents.preview_edit": set.documentsPreviewEdit,
		"documents.update":       set.documentsUpdate,
		"documents.save":         set.documentsSave,
		"graphs.validate":        set.graphsValidate,
		"runs.preflight":         set.runsPreflight,
	}
}

func (h *documentHandlerSet) ready() *ServiceError { return h.recovery }

func documentError(err error) *ServiceError {
	switch {
	case errors.Is(err, ErrDocumentNotFound), errors.Is(err, ErrRegistryNotFound):
		return protocolError("not_found", "Graph document or registered item not found")
	case errors.Is(err, ErrAuthoringReferences):
		return protocolError("conflict", "Node has dependent graph references; confirm their removal")
	case errors.Is(err, ErrAuthoringUnknownLayout):
		return protocolError("conflict", "Layout metadata has an unknown version; confirm replacement")
	case errors.Is(err, ErrDocumentConflict), errors.Is(err, ErrDocumentUnsaved):
		return protocolError("conflict", "Graph document revision or saved file changed")
	case errors.Is(err, ErrAuthoringAlias):
		return protocolError("invalid_request", "This YAML alias cannot be edited visually; edit the source directly")
	case errors.Is(err, ErrAuthoringInvalid):
		return protocolError("invalid_request", "Correct invalid graph YAML before visual editing")
	case errors.Is(err, ErrInvalidDocumentPath), errors.Is(err, ErrDocumentTooLarge), errors.Is(err, ErrDocumentInvalidEncoding), errors.Is(err, ErrInvalidGraphID), errors.Is(err, ErrAuthoringOperation):
		return protocolError("invalid_request", "Invalid graph document path or text")
	case errors.Is(err, ErrDocumentCreateUnavailable):
		return protocolError("unavailable", "Safe graph creation is unavailable on this platform")
	case errors.Is(err, ErrDocumentSaveInDoubt):
		return protocolError("unavailable", "Graph save requires recovery before further work")
	case errors.Is(err, ErrAuthoringResultTooLarge):
		return protocolError("response_too_large", "Graph authoring result exceeds response limit")
	default:
		return protocolError("unavailable", "Graph document or local service is unavailable")
	}
}

func (h *documentHandlerSet) documentsCreate(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "project_id", "relative_path", "graph_id")
	if e != nil {
		return nil, e
	}
	var projectID, relative, graphID string
	if requiredField(fields, "project_id", &projectID) != nil || !validSaveID(projectID) ||
		requiredField(fields, "relative_path", &relative) != nil || !validRelativeDocumentPath(relative) ||
		requiredField(fields, "graph_id", &graphID) != nil || !validGraphIdentifier(graphID) {
		return nil, protocolError("invalid_request", "Invalid graph creation request")
	}
	doc, err := h.documents.Create(projectID, relative, graphID)
	if err != nil {
		return nil, documentError(err)
	}
	return toDocumentDTO(doc), nil
}

func (h *documentHandlerSet) documentsReload(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "expected_revision", "discard_draft")
	if e != nil {
		return nil, e
	}
	var id string
	var revision uint64
	var discard bool
	if requiredField(fields, "document_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "expected_revision", &revision) != nil || revision == 0 ||
		requiredField(fields, "discard_draft", &discard) != nil {
		return nil, protocolError("invalid_request", "Invalid graph reload request")
	}
	doc, err := h.documents.Reload(id, revision, discard)
	if err != nil {
		return nil, documentError(err)
	}
	return toDocumentDTO(doc), nil
}

func (h *documentHandlerSet) documentsInspect(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "revision")
	if e != nil {
		return nil, e
	}
	var id string
	var revision uint64
	if requiredField(fields, "document_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "revision", &revision) != nil || revision == 0 {
		return nil, protocolError("invalid_request", "Invalid graph inspection request")
	}
	result, err := h.documents.Inspect(id, revision)
	if err != nil {
		return nil, documentError(err)
	}
	return result, nil
}

func (h *documentHandlerSet) documentsPreviewEdit(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "revision", "operation")
	if e != nil {
		return nil, e
	}
	var id string
	var revision uint64
	if requiredField(fields, "document_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "revision", &revision) != nil || revision == 0 {
		return nil, protocolError("invalid_request", "Invalid graph preview request")
	}
	op, e := decodeAuthoringOperation(fields["operation"])
	if e != nil {
		return nil, e
	}
	result, err := h.documents.PreviewEdit(id, revision, op)
	if err != nil {
		return nil, documentError(err)
	}
	return result, nil
}

func decodeAuthoringOperation(raw json.RawMessage) (AuthoringOperation, *ServiceError) {
	fields, e := strictParams(raw, "kind", "node_id", "node_type", "remove_references", "replace_unknown_layout", "x", "y")
	if e != nil {
		return AuthoringOperation{}, e
	}
	var op AuthoringOperation
	if requiredField(fields, "kind", &op.Kind) != nil || requiredField(fields, "node_id", &op.NodeID) != nil || !validGraphIdentifier(op.NodeID) {
		return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
	}
	allowed := map[string]bool{"kind": true, "node_id": true}
	switch op.Kind {
	case "add_node":
		allowed["node_type"] = true
		if requiredField(fields, "node_type", &op.NodeType) != nil ||
			(op.NodeType != "shell" && op.NodeType != "agent" && op.NodeType != "gate") {
			return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
		}
	case "delete_node":
		allowed["remove_references"] = true
		if requiredField(fields, "remove_references", &op.RemoveReferences) != nil {
			return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
		}
	case "set_layout":
		allowed["x"], allowed["y"], allowed["replace_unknown_layout"] = true, true, true
		if requiredField(fields, "x", &op.X) != nil || requiredField(fields, "y", &op.Y) != nil ||
			op.X < -maxLayoutCoordinate || op.X > maxLayoutCoordinate || op.Y < -maxLayoutCoordinate || op.Y > maxLayoutCoordinate {
			return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
		}
		if _, ok := fields["replace_unknown_layout"]; ok && requiredField(fields, "replace_unknown_layout", &op.ReplaceUnknownLayout) != nil {
			return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
		}
	case "remove_layout":
		allowed["replace_unknown_layout"] = true
		if _, ok := fields["replace_unknown_layout"]; ok && requiredField(fields, "replace_unknown_layout", &op.ReplaceUnknownLayout) != nil {
			return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
		}
	default:
		return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
	}
	for name := range fields {
		if !allowed[name] {
			return AuthoringOperation{}, protocolError("invalid_request", "Invalid graph edit operation")
		}
	}
	return op, nil
}

func (h *documentHandlerSet) documentsOpen(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "project_id", "relative_path")
	if e != nil {
		return nil, e
	}
	var projectID, relative string
	if requiredField(fields, "project_id", &projectID) != nil || !validSaveID(projectID) ||
		requiredField(fields, "relative_path", &relative) != nil || !validRelativeDocumentPath(relative) {
		return nil, protocolError("invalid_request", "Invalid graph document selection")
	}
	doc, err := h.documents.Open(projectID, relative)
	if err != nil {
		return nil, documentError(err)
	}
	return toDocumentDTO(doc), nil
}

func (h *documentHandlerSet) documentsUpdate(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "expected_revision", "text_base64")
	if e != nil {
		return nil, e
	}
	var id, encodedText string
	var revision uint64
	if requiredField(fields, "document_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "expected_revision", &revision) != nil || revision == 0 ||
		requiredField(fields, "text_base64", &encodedText) != nil ||
		len(encodedText) > base64.StdEncoding.EncodedLen(MaxDocumentBytes) {
		return nil, protocolError("invalid_request", "Invalid graph document update")
	}
	text, err := base64.StdEncoding.Strict().DecodeString(encodedText)
	if err != nil || len(text) > MaxDocumentBytes {
		return nil, protocolError("invalid_request", "Invalid graph document encoding")
	}
	doc, err := h.documents.Update(id, revision, string(text))
	if err != nil {
		return nil, documentError(err)
	}
	return toDocumentDTO(doc), nil
}

func (h *documentHandlerSet) documentsSave(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "expected_revision", "expected_disk_digest", "accept_reformat")
	if e != nil {
		return nil, e
	}
	var id, digest string
	var revision uint64
	var reformat bool
	if requiredField(fields, "document_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "expected_revision", &revision) != nil || revision == 0 ||
		requiredField(fields, "expected_disk_digest", &digest) != nil || !validDigest(digest) ||
		requiredField(fields, "accept_reformat", &reformat) != nil {
		return nil, protocolError("invalid_request", "Invalid graph document save")
	}
	doc, err := h.documents.Save(id, revision, digest, reformat)
	if err != nil {
		return nil, documentError(err)
	}
	return toDocumentDTO(doc), nil
}

func (h *documentHandlerSet) graphsValidate(_ context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "revision")
	if e != nil {
		return nil, e
	}
	var id string
	var revision uint64
	if requiredField(fields, "document_id", &id) != nil || !validSaveID(id) ||
		requiredField(fields, "revision", &revision) != nil || revision == 0 {
		return nil, protocolError("invalid_request", "Invalid graph validation revision")
	}
	doc, err := h.documents.Get(id, revision)
	if err != nil {
		return nil, documentError(err)
	}
	return summarizeDiagnostics(doc.Diagnostics), nil
}

func (h *documentHandlerSet) runsPreflight(ctx context.Context, raw json.RawMessage) (any, *ServiceError) {
	if e := h.ready(); e != nil {
		return nil, e
	}
	fields, e := strictParams(raw, "document_id", "revision", "workspace_id", "inputs", "max_concurrency")
	if e != nil {
		return nil, e
	}
	var request PreflightRequest
	if requiredField(fields, "document_id", &request.DocumentID) != nil || !validSaveID(request.DocumentID) ||
		requiredField(fields, "revision", &request.Revision) != nil || request.Revision == 0 ||
		requiredField(fields, "workspace_id", &request.WorkspaceID) != nil || !validSaveID(request.WorkspaceID) ||
		requiredField(fields, "max_concurrency", &request.MaxConcurrency) != nil ||
		request.MaxConcurrency < 1 || request.MaxConcurrency > 32 {
		return nil, protocolError("invalid_request", "Invalid preflight selection")
	}
	inputs, err := decodeTypedInputs(fields["inputs"])
	if err != nil {
		return nil, protocolError("invalid_request", "Invalid typed graph inputs")
	}
	request.Inputs = inputs
	result, err := h.preflight.Preflight(ctx, request)
	if err != nil {
		return nil, documentError(err)
	}
	return result, nil
}
