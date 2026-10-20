package desktop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/agent/codex"
	"agentplane/internal/events"
	"agentplane/internal/gate"
	"agentplane/internal/gitstate"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
)

const preflightTokenLifetime = 120 * time.Second
const maxPreflightTokens = 8
const maxPreflightSnapshotBytes = 16 << 20

// CapabilityDetector is the consumer-owned seam for bounded tool detection.
// Implementations must not execute graph commands or agent tasks.
type CapabilityDetector interface {
	DetectCodex(context.Context, string) (agent.CapabilityReport, error)
	CheckGit(context.Context, string, string) error
}

type installedCapabilities struct{}

func (installedCapabilities) DetectCodex(ctx context.Context, path string) (agent.CapabilityReport, error) {
	return (codex.Adapter{Binary: path}).Detect(ctx)
}

func (installedCapabilities) CheckGit(ctx context.Context, workspace, configured string) error {
	canonical, err := validateToolPath(configured)
	if err != nil {
		return err
	}
	runtimeGit, err := filepath.EvalSymlinks("/usr/bin/git")
	if err != nil || canonical != runtimeGit {
		return ErrToolUnavailable
	}
	root, err := gitstate.CanonicalWorkspace(ctx, workspace)
	if err != nil || root != workspace {
		return ErrToolUnavailable
	}
	return nil
}

type PreflightRequest struct {
	DocumentID     string
	Revision       uint64
	WorkspaceID    string
	Inputs         map[string]graph.Value
	MaxConcurrency int
}

type PreflightResult struct {
	DiagnosticSummary
	PreflightToken string `json:"preflight_token,omitempty"`
	GraphVersion   string `json:"graph_version,omitempty"`
	PayloadDigest  string `json:"payload_digest,omitempty"`
}

type PreflightTicket struct {
	Document        SavedDocumentSnapshot
	Workspace       ResolvedWorkspace
	Inputs          map[string]graph.Value
	MaxConcurrency  int
	Settings        LocalSettings
	CapabilityHash  string
	PayloadDigest   string
	ExpiresAt       time.Time
	creationOrdinal uint64
}

type PreflightService struct {
	registry  *Registry
	documents *DocumentStore
	detector  CapabilityDetector
	mu        sync.Mutex
	tokens    map[string]PreflightTicket
	ordinal   uint64
	bytes     int
	now       func() time.Time
}

func NewPreflightService(registry *Registry, documents *DocumentStore, detector CapabilityDetector) *PreflightService {
	if detector == nil {
		detector = installedCapabilities{}
	}
	return &PreflightService{registry: registry, documents: documents, detector: detector,
		tokens: make(map[string]PreflightTicket), now: time.Now}
}

func (s *PreflightService) Preflight(ctx context.Context, request PreflightRequest) (PreflightResult, error) {
	if ctx == nil {
		return PreflightResult{}, errors.New("preflight context is nil")
	}
	if s == nil || s.registry == nil || s.documents == nil || s.detector == nil {
		return PreflightResult{}, ErrRegistryUnavailable
	}
	doc, err := s.documents.Get(request.DocumentID, request.Revision)
	if err != nil {
		return PreflightResult{}, err
	}
	findings := append([]graph.Diagnostic(nil), doc.Diagnostics...)
	if doc.Validity != DocumentValid {
		return PreflightResult{DiagnosticSummary: summarizeDiagnostics(findings)}, nil
	}
	snapshot, err := s.documents.SavedSnapshot(request.DocumentID, request.Revision)
	if err != nil {
		switch {
		case errors.Is(err, ErrDocumentUnsaved):
			return preflightFailure("E_DOCUMENT_NOT_SAVED", "Save the valid graph before starting", "document_id"), nil
		case errors.Is(err, ErrDocumentSaveInDoubt):
			return preflightFailure("E_DOCUMENT_RECOVERY", "Graph save requires recovery before starting", "document_id"), nil
		case errors.Is(err, ErrDocumentConflict):
			return preflightFailure("E_DOCUMENT_CHANGED", "Graph file changed; reload or resolve the conflict", "document_id"), nil
		default:
			return preflightFailure("E_DOCUMENT_UNAVAILABLE", "Saved graph file is unavailable", "document_id"), nil
		}
	}
	definition, err := graph.Parse([]byte(snapshot.Text))
	if err != nil {
		return preflightFailure("E_GRAPH_PARSE", "Graph YAML cannot be parsed", "document_id"), nil
	}
	plan, validation := graph.CompileBindings(definition)
	findings = append([]graph.Diagnostic(nil), validation.Diagnostics...)
	if plan == nil {
		return PreflightResult{DiagnosticSummary: summarizeDiagnostics(findings)}, nil
	}
	findings = append(findings, plan.ValidateGraphInputs(request.Inputs).Diagnostics...)
	workspace, err := s.registry.ResolveWorkspace(request.WorkspaceID)
	if err != nil {
		findings = append(findings, preflightDiagnostic(definition.ID, "", "workspace_id", "E_WORKSPACE_UNAVAILABLE", "Selected workspace is unavailable"))
	} else if workspace.ProjectID != snapshot.ProjectID {
		findings = append(findings, preflightDiagnostic(definition.ID, "", "workspace_id", "E_WORKSPACE_PROJECT", "Selected workspace belongs to another project"))
	}
	settings, err := s.registry.Settings()
	if err != nil {
		return PreflightResult{}, err
	}
	if request.MaxConcurrency < 1 || request.MaxConcurrency > settings.MaxConcurrency {
		findings = append(findings, preflightDiagnostic(definition.ID, "", "max_concurrency", "E_CONCURRENCY_LIMIT", "Concurrency exceeds local setting or is below one"))
	}
	if (graph.ValidationResult{Diagnostics: findings}).HasErrors() {
		return PreflightResult{DiagnosticSummary: summarizeDiagnostics(findings)}, nil
	}
	capabilityHash, checks := s.checkNodes(ctx, definition, workspace, settings)
	findings = append(findings, checks...)
	result := PreflightResult{DiagnosticSummary: summarizeDiagnostics(findings)}
	if result.HasErrors {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return PreflightResult{}, err
	}
	encodedInputs, err := encodeTypedInputsExact(request.Inputs)
	if err != nil {
		return preflightFailure("E_INPUT_VALUE_INVALID", "Graph input value is invalid", "inputs"), nil
	}
	payload := payloadDigest(snapshot, workspace, settings, request.MaxConcurrency, encodedInputs, capabilityHash)
	token, err := s.storeToken(PreflightTicket{Document: snapshot, Workspace: workspace,
		Inputs: cloneInputValues(request.Inputs), MaxConcurrency: request.MaxConcurrency,
		Settings: cloneSettings(settings), CapabilityHash: capabilityHash, PayloadDigest: payload})
	if err != nil {
		return PreflightResult{}, err
	}
	result.PreflightToken, result.GraphVersion, result.PayloadDigest = token, snapshot.SemanticVersion, payload
	return result, nil
}

func preflightFailure(code, message, path string) PreflightResult {
	return PreflightResult{DiagnosticSummary: summarizeDiagnostics([]graph.Diagnostic{preflightDiagnostic("", "", path, code, message)})}
}

func preflightDiagnostic(graphID, nodeID, path, code, message string) graph.Diagnostic {
	return graph.Diagnostic{Severity: graph.SeverityError, Code: code, Message: message,
		GraphID: graphID, Location: graph.DiagnosticLocation{NodeID: nodeID, Path: path}}
}

func (s *PreflightService) checkNodes(ctx context.Context, definition *graph.Definition, workspace ResolvedWorkspace, settings LocalSettings) (string, []graph.Diagnostic) {
	ids := make([]string, 0, len(definition.Nodes))
	for id := range definition.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var findings []graph.Diagnostic
	needsAgent, needsGit := false, false
	for _, id := range ids {
		node := definition.Nodes[id]
		var err error
		switch node.Type {
		case "shell":
			err = (shell.Executor{}).ValidateNodeConfig(id, node)
		case "gate":
			err = (gate.Executor{}).ValidateNodeConfig(id, node)
			if node.Config["kind"] == "changed_files_scope" {
				needsGit = true
			}
		case "agent":
			err = (agent.Executor{}).ValidateNodeConfig(id, node)
			needsAgent, needsGit = true, true
			if node.Config["adapter"] != "codex" {
				findings = append(findings, preflightDiagnostic(definition.ID, id, "nodes."+id+".config.adapter", "E_ADAPTER_UNSUPPORTED", "Only the configured Codex adapter is supported"))
			}
		default:
			findings = append(findings, preflightDiagnostic(definition.ID, id, "nodes."+id+".type", "E_NODE_TYPE_UNSUPPORTED", "Node type is not supported by this desktop service"))
		}
		if err != nil {
			findings = append(findings, preflightDiagnostic(definition.ID, id, "nodes."+id+".config", "E_NODE_CONFIG", "Node configuration is invalid"))
		}
		if node.Type == "agent" || node.Type == "gate" && node.Config["kind"] == "changed_files_scope" {
			if node.Config["workspace"] != workspace.CanonicalPath {
				findings = append(findings, preflightDiagnostic(definition.ID, id, "nodes."+id+".config.workspace", "E_NODE_WORKSPACE", "Node workspace differs from selected workspace"))
			}
		}
		if node.Loop != nil && node.Loop.Gate.Config["kind"] == "changed_files_scope" {
			needsGit = true
		}
	}
	if (graph.ValidationResult{Diagnostics: findings}).HasErrors() {
		return "", findings
	}
	if err := runtime.ValidateLoopConfigs(definition, loopCapabilityExecutor{}); err != nil {
		findings = append(findings, preflightDiagnostic(definition.ID, "", "nodes", "E_LOOP_PREFLIGHT", "Loop configuration is invalid"))
		return "", findings
	}
	var report agent.CapabilityReport
	if needsAgent {
		if settings.CodexBinaryPath == nil {
			findings = append(findings, preflightDiagnostic(definition.ID, "", "settings.codex_binary_path", "E_ADAPTER_UNAVAILABLE", "Configure a supported Codex executable"))
		} else {
			var err error
			report, err = s.detector.DetectCodex(ctx, *settings.CodexBinaryPath)
			if err != nil || !report.Available || report.Workspace == nil || !*report.Workspace {
				findings = append(findings, preflightDiagnostic(definition.ID, "", "settings.codex_binary_path", "E_ADAPTER_UNAVAILABLE", "Configured Codex capability is unavailable"))
			}
		}
	}
	if needsGit {
		if settings.GitBinaryPath == nil || s.detector.CheckGit(ctx, workspace.CanonicalPath, derefPath(settings.GitBinaryPath)) != nil {
			findings = append(findings, preflightDiagnostic(definition.ID, "", "settings.git_binary_path", "E_GIT_UNAVAILABLE", "Configured Git executable or worktree is unavailable"))
		}
	}
	if (graph.ValidationResult{Diagnostics: findings}).HasErrors() {
		return "", findings
	}
	if err := runtime.PreflightLoops(ctx, definition, loopCapabilityExecutor{report: report}); err != nil {
		findings = append(findings, preflightDiagnostic(definition.ID, "", "nodes", "E_LOOP_PREFLIGHT", "Loop configuration or correction capability is unavailable"))
		return "", findings
	}
	encoded, _ := json.Marshal(struct {
		CodexPath *string                `json:"codex_path,omitempty"`
		GitPath   *string                `json:"git_path,omitempty"`
		Report    agent.CapabilityReport `json:"report"`
	}{settings.CodexBinaryPath, settings.GitBinaryPath, report})
	return digestBytes(encoded), findings
}

func derefPath(path *string) string {
	if path == nil {
		return ""
	}
	return *path
}

// loopCapabilityExecutor is used only by runtime.PreflightLoops. Execution
// methods fail closed if accidentally called.
type loopCapabilityExecutor struct{ report agent.CapabilityReport }

func (loopCapabilityExecutor) ExecuteNode(context.Context, string, graph.NodeDefinition, map[string]graph.Value) (map[string]graph.Value, error) {
	return nil, errors.New("preflight executor cannot execute nodes")
}
func (loopCapabilityExecutor) ExecuteNodeObserved(context.Context, string, graph.NodeDefinition, map[string]graph.Value, runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	return nil, nil, errors.New("preflight executor cannot execute nodes")
}
func (loopCapabilityExecutor) ValidateNodeConfig(id string, node graph.NodeDefinition) error {
	if node.Type == "gate" {
		return (gate.Executor{}).ValidateNodeConfig(id, node)
	}
	return (agent.Executor{}).ValidateNodeConfig(id, node)
}
func (e loopCapabilityExecutor) PreflightNode(context.Context, string, graph.NodeDefinition) error {
	if !e.report.Available || e.report.Workspace == nil || !*e.report.Workspace ||
		e.report.CorrectionFeedback == nil || !*e.report.CorrectionFeedback {
		return ErrToolUnavailable
	}
	return nil
}

func payloadDigest(doc SavedDocumentSnapshot, workspace ResolvedWorkspace, settings LocalSettings, concurrency int, inputs []byte, capabilities string) string {
	var data []byte
	appendField := func(value string) {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		data = append(data, length[:]...)
		data = append(data, value...)
	}
	for _, value := range []string{"agentplane-preflight-v1", doc.DocumentID, doc.ProjectID, doc.RelativePath,
		fmt.Sprint(doc.Revision), doc.DiskDigest, string(doc.RootIdentity), string(doc.FileIdentity),
		doc.SemanticVersion, workspace.WorkspaceID, workspace.ProjectID, workspace.CanonicalPath,
		string(workspace.Identity), fmt.Sprint(settings.Revision), fmt.Sprint(concurrency), capabilities, string(inputs)} {
		appendField(value)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

func cloneInputValues(inputs map[string]graph.Value) map[string]graph.Value {
	returnValue := make(map[string]graph.Value, len(inputs))
	for key, value := range inputs {
		if raw, ok := value.AsJSON(); ok {
			value, _ = graph.NewJSONValue(raw)
		}
		returnValue[key] = value
	}
	return returnValue
}

func (s *PreflightService) storeToken(ticket PreflightTicket) (string, error) {
	if len(ticket.Document.Text) > maxPreflightSnapshotBytes {
		return "", ErrDocumentTooLarge
	}
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(entropy[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, existing := range s.tokens {
		if !now.Before(existing.ExpiresAt) {
			s.deleteToken(key)
		}
	}
	for len(s.tokens) >= maxPreflightTokens || s.bytes+len(ticket.Document.Text) > maxPreflightSnapshotBytes {
		oldest := ""
		for key, existing := range s.tokens {
			if oldest == "" || existing.creationOrdinal < s.tokens[oldest].creationOrdinal ||
				existing.creationOrdinal == s.tokens[oldest].creationOrdinal && key < oldest {
				oldest = key
			}
		}
		if oldest == "" {
			return "", ErrDocumentTooLarge
		}
		s.deleteToken(oldest)
	}
	s.ordinal++
	ticket.creationOrdinal = s.ordinal
	ticket.ExpiresAt = now.Add(preflightTokenLifetime)
	s.tokens[token] = ticket
	s.bytes += len(ticket.Document.Text)
	return token, nil
}

func (s *PreflightService) deleteToken(token string) {
	s.bytes -= len(s.tokens[token].Document.Text)
	delete(s.tokens, token)
}

func (s *PreflightService) ticket(token string) (PreflightTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tokens[token]
	if !ok || !s.now().Before(ticket.ExpiresAt) {
		if ok {
			s.deleteToken(token)
		}
		return PreflightTicket{}, false
	}
	ticket.Inputs = cloneInputValues(ticket.Inputs)
	ticket.Settings = cloneSettings(ticket.Settings)
	return ticket, true
}
