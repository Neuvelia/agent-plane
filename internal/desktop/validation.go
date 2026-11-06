package desktop

import (
	"encoding/json"

	"agentplane/internal/graph"
)

const maxProtocolDiagnostics = 1000
const maxDiagnosticResponseBytes = 1 << 20

// DiagnosticDTO keeps locations flat for renderer consumers. The current YAML
// parser does not retain source positions, so line and column remain absent.
type DiagnosticDTO struct {
	Code                   string         `json:"code"`
	Severity               graph.Severity `json:"severity"`
	Message                string         `json:"message"`
	GraphID                string         `json:"graph_id,omitempty"`
	NodeID                 string         `json:"node_id,omitempty"`
	EdgeIndex              *int           `json:"edge_index,omitempty"`
	GraphInputBindingIndex *int           `json:"graph_input_binding_index,omitempty"`
	Path                   string         `json:"path,omitempty"`
	Line                   *int           `json:"line,omitempty"`
	Column                 *int           `json:"column,omitempty"`
	Suggestion             string         `json:"suggestion,omitempty"`
}

type DiagnosticSummary struct {
	Diagnostics        []DiagnosticDTO `json:"diagnostics"`
	DiagnosticsTotal   int             `json:"diagnostics_total"`
	DiagnosticsOmitted int             `json:"diagnostics_omitted"`
	HasErrors          bool            `json:"has_errors"`
}

func summarizeDiagnostics(findings []graph.Diagnostic) DiagnosticSummary {
	count := len(findings)
	summary := DiagnosticSummary{Diagnostics: make([]DiagnosticDTO, 0, min(count, maxProtocolDiagnostics)),
		DiagnosticsTotal: count}
	usedBytes := 0
	stopped := false
	for i, finding := range findings {
		if finding.Severity == graph.SeverityError {
			summary.HasErrors = true
		}
		if i >= maxProtocolDiagnostics || stopped {
			continue
		}
		message, suggestion := safeDiagnosticText(finding.Code, finding.Severity)
		dto := DiagnosticDTO{
			Code: finding.Code, Severity: finding.Severity, Message: message,
			GraphID: finding.GraphID, NodeID: finding.Location.NodeID,
			EdgeIndex:              copyOptionalInt(finding.Location.EdgeIndex),
			GraphInputBindingIndex: copyOptionalInt(finding.Location.GraphInputBindingIndex),
			Path:                   finding.Location.Path, Suggestion: suggestion,
		}
		encoded, err := json.Marshal(dto)
		if err != nil || usedBytes+len(encoded) > maxDiagnosticResponseBytes {
			stopped = true
			continue
		}
		usedBytes += len(encoded)
		summary.Diagnostics = append(summary.Diagnostics, dto)
	}
	summary.DiagnosticsOmitted = count - len(summary.Diagnostics)
	return summary
}

// Graph validators include authored identifiers and references in their text.
// Protocol text is fixed by code; locations still identify the authored field.
func safeDiagnosticText(code string, severity graph.Severity) (string, string) {
	switch code {
	case "E_GRAPH_PARSE":
		return "Graph YAML cannot be parsed", "Correct YAML syntax and schema_version"
	case "E_GRAPH_ID_REQUIRED", "E_GRAPH_ID_INVALID", "E_IDENTIFIER_INVALID":
		return "Graph identifier is missing or invalid", "Use lowercase letters, digits, hyphens or underscores"
	case "E_GRAPH_CYCLE":
		return "Graph contains a dependency cycle", "Remove a cyclic binding"
	case "E_GRAPH_INPUT_VALUE_MISSING", "E_GRAPH_INPUT_VALUE_TYPE_MISMATCH", "E_GRAPH_INPUT_VALUE_UNKNOWN":
		return "Graph input does not match its declaration", "Supply declared inputs with their exact types"
	case "E_EDGE_SOURCE", "E_EDGE_TARGET", "E_EDGE_SOURCE_REFERENCE_INVALID", "E_EDGE_TARGET_REFERENCE_INVALID",
		"E_GRAPH_INPUT_REFERENCE_INVALID", "E_GRAPH_INPUT_NOT_FOUND", "E_GRAPH_INPUT_TARGET_INPUT_NOT_FOUND",
		"E_GRAPH_INPUT_TARGET_NODE_NOT_FOUND", "E_GRAPH_INPUT_TARGET_REFERENCE_INVALID",
		"E_GRAPH_OUTPUT_REFERENCE_INVALID", "E_GRAPH_OUTPUT_SOURCE_NODE_NOT_FOUND", "E_GRAPH_OUTPUT_SOURCE_OUTPUT_NOT_FOUND":
		return "Graph binding reference cannot be resolved", "Select a declared node and port"
	case "E_REQUIRED_INPUT_UNBOUND", "E_INPUT_MULTIPLE_PROVIDERS", "E_BINDING_TYPE_MISMATCH", "E_VALUE_TYPE_INVALID":
		return "Graph port binding violates its contract", "Check port types and providers"
	case "E_DOCUMENT_NOT_SAVED":
		return "Graph has unsaved changes", "Save the valid graph before starting"
	case "E_DOCUMENT_CHANGED":
		return "Saved graph file changed", "Reload or resolve the file conflict"
	case "E_DOCUMENT_RECOVERY":
		return "Graph save requires recovery", "Restart after resolving the saved draft"
	case "E_DOCUMENT_UNAVAILABLE":
		return "Saved graph file is unavailable", "Restore access to the registered file"
	case "E_WORKSPACE_UNAVAILABLE", "E_WORKSPACE_PROJECT", "E_NODE_WORKSPACE":
		return "Selected workspace does not match the graph", "Select an available workspace in this project"
	case "E_CONCURRENCY_LIMIT":
		return "Concurrency exceeds the local setting", "Reduce concurrency or update local settings"
	case "E_NODE_CONFIG", "E_LOOP_PREFLIGHT":
		return "Node or loop configuration is invalid", "Correct the indicated node configuration"
	case "E_ADAPTER_UNAVAILABLE", "E_ADAPTER_UNSUPPORTED", "E_GIT_UNAVAILABLE":
		return "Required local tool is unavailable or unsupported", "Check the configured executable and workspace"
	case "E_ENVIRONMENT_UNAVAILABLE":
		return "A referenced environment value is unavailable or invalid", "Configure the named local value before preflight"
	case "E_REDACTION_LIMIT":
		return "Launch redaction values exceed local limits", "Reduce environment or graph input values"
	case "E_EMBEDDED_SECRET":
		return "Graph or launch inputs contain a credential-shaped key", "Remove or rename the key; keep credentials outside graph files and launch inputs"
	case "E_NODE_TYPE_UNSUPPORTED":
		return "Node type is unsupported by the desktop service", "Use a supported node type"
	case "E_INPUT_VALUE_INVALID":
		return "Graph input value is invalid", "Supply the declared typed value"
	case "W_DECLARED_OUTPUT_UNUSED":
		return "Declared output is unused", "Remove it or bind it to a consumer"
	default:
		if severity == graph.SeverityWarning {
			return "Graph validation warning at the indicated path", "Inspect the diagnostic code and path"
		}
		return "Graph validation error at the indicated path", "Inspect the diagnostic code and path"
	}
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
