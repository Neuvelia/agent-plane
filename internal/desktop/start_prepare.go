package desktop

import (
	"bytes"
	"context"
	"reflect"

	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

// PrepareStart rechecks a previously green token against disk, registry,
// environment and installed tools immediately before RunService reserves a
// launch. Every returned object is detached from the editable document buffer.
func (s *PreflightService) PrepareStart(ctx context.Context, request RunStartRequest) (PreparedRun, *ServiceError) {
	if s == nil || s.registry == nil || s.documents == nil || s.sealErr != nil {
		return PreparedRun{}, protocolError("unavailable", "Preflight service is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return PreparedRun{}, protocolError("unavailable", "Start request was interrupted")
	}
	ticket, ok := s.ticket(request.PreflightToken)
	if !ok {
		return PreparedRun{}, protocolError("conflict", "Preflight token expired or was evicted")
	}
	if request.DocumentID != ticket.Document.DocumentID || request.Revision != ticket.Document.Revision ||
		request.WorkspaceID != ticket.Workspace.WorkspaceID || request.MaxConcurrency != ticket.MaxConcurrency ||
		request.PayloadDigest != ticket.PayloadDigest {
		return PreparedRun{}, protocolError("conflict", "Start payload differs from preflight")
	}
	requestedInputs, err := encodeTypedInputsExact(request.Inputs)
	if err != nil {
		return PreparedRun{}, protocolError("invalid_request", "Invalid typed graph inputs")
	}
	ticketInputs, err := encodeTypedInputsExact(ticket.Inputs)
	if err != nil || !bytes.Equal(requestedInputs, ticketInputs) {
		return PreparedRun{}, protocolError("conflict", "Start inputs differ from preflight")
	}
	snapshot, err := s.documents.SavedSnapshot(request.DocumentID, request.Revision)
	if err != nil || snapshot != ticket.Document {
		return PreparedRun{}, protocolError("conflict", "Saved graph changed since preflight")
	}
	workspace, err := s.registry.ResolveWorkspace(request.WorkspaceID)
	if err != nil || workspace != ticket.Workspace || workspace.ProjectID != snapshot.ProjectID {
		return PreparedRun{}, protocolError("conflict", "Workspace changed since preflight")
	}
	settings, err := s.registry.Settings()
	if err != nil || !reflect.DeepEqual(settings, ticket.Settings) ||
		request.MaxConcurrency < 1 || request.MaxConcurrency > settings.MaxConcurrency {
		return PreparedRun{}, protocolError("conflict", "Local settings changed since preflight")
	}
	definition, err := graph.Parse([]byte(snapshot.Text))
	if err != nil {
		return PreparedRun{}, protocolError("conflict", "Saved graph cannot be parsed")
	}
	plan, validation := graph.CompileBindings(definition)
	if plan == nil || validation.HasErrors() || plan.ValidateGraphInputs(request.Inputs).HasErrors() {
		return PreparedRun{}, protocolError("conflict", "Graph contract changed since preflight")
	}
	capabilityHash, environment, findings := s.checkNodesWithEnvironment(ctx, definition, workspace, settings)
	if (graph.ValidationResult{Diagnostics: findings}).HasErrors() || capabilityHash != ticket.CapabilityHash {
		return PreparedRun{}, protocolError("conflict", "Local capability changed since preflight")
	}
	if payloadDigest(snapshot, workspace, settings, request.MaxConcurrency, requestedInputs, capabilityHash) != ticket.PayloadDigest {
		return PreparedRun{}, protocolError("conflict", "Start payload changed since preflight")
	}
	// Capability probes and binary hashing can take seconds. Recheck mutable
	// local state after them so a save or registry edit during probing cannot
	// launch an already stale snapshot.
	latestDocument, err := s.documents.SavedSnapshot(request.DocumentID, request.Revision)
	if err != nil || latestDocument != snapshot {
		return PreparedRun{}, protocolError("conflict", "Saved graph changed during capability checks")
	}
	latestWorkspace, err := s.registry.ResolveWorkspace(request.WorkspaceID)
	if err != nil || latestWorkspace != workspace {
		return PreparedRun{}, protocolError("conflict", "Workspace changed during capability checks")
	}
	latestSettings, err := s.registry.Settings()
	if err != nil || !reflect.DeepEqual(latestSettings, settings) {
		return PreparedRun{}, protocolError("conflict", "Local settings changed during capability checks")
	}
	redactions, err := launchRedactions(environment, request.Inputs)
	if err != nil {
		return PreparedRun{}, protocolError("conflict", "Launch redaction values changed since preflight")
	}
	if err := ctx.Err(); err != nil {
		return PreparedRun{}, protocolError("unavailable", "Start request was interrupted")
	}
	executor := desktopNodeExecutor{shell: shellExecutorFor(environment), agents: agentExecutorFor(settings, environment)}
	return PreparedRun{Definition: definition, Inputs: cloneInputValues(request.Inputs), Executor: executor,
		Options:      runtime.Options{MaxConcurrency: request.MaxConcurrency, LogRedactions: redactions},
		GraphVersion: snapshot.SemanticVersion}, nil
}
