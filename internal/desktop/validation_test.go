package desktop

import (
	"encoding/json"
	"strings"
	"testing"

	"agentplane/internal/graph"
)

func TestGraphDiagnosticsMatchGraphValidatorAndFlattenLocation(t *testing.T) {
	text := []byte(`schema_version: 0
id: example
inputs: {}
nodes:
  producer:
    type: shell
    outputs:
      stdout: {type: string}
edges:
  - from: missing.outputs.stdout
    to: producer.inputs.value
outputs: {}
`)
	definition, err := graph.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	want := graph.Validate(definition)
	got := summarizeDiagnostics(want.Diagnostics)
	if got.DiagnosticsTotal != len(want.Diagnostics) || got.DiagnosticsOmitted != 0 || got.HasErrors != want.HasErrors() {
		t.Fatalf("summary = %#v, validator = %#v", got, want)
	}
	for i, diagnostic := range want.Diagnostics {
		if got.Diagnostics[i].Code != diagnostic.Code || got.Diagnostics[i].Severity != diagnostic.Severity ||
			got.Diagnostics[i].Path != diagnostic.Location.Path || got.Diagnostics[i].NodeID != diagnostic.Location.NodeID {
			t.Fatalf("diagnostic %d = %#v, want %#v", i, got.Diagnostics[i], diagnostic)
		}
		if got.Diagnostics[i].Line != nil || got.Diagnostics[i].Column != nil {
			t.Fatalf("invented source position: %#v", got.Diagnostics[i])
		}
	}
}

func TestGraphDiagnosticsTruncationPreservesBlockingStatus(t *testing.T) {
	findings := make([]graph.Diagnostic, 1002)
	for i := 0; i < 1001; i++ {
		findings[i] = graph.Diagnostic{Severity: graph.SeverityWarning, Code: "W_TEST"}
	}
	findings[1001] = graph.Diagnostic{Severity: graph.SeverityError, Code: "E_LAST"}
	got := summarizeDiagnostics(findings)
	if len(got.Diagnostics) != 1000 || got.DiagnosticsTotal != 1002 || got.DiagnosticsOmitted != 2 || !got.HasErrors {
		t.Fatalf("truncated summary = %#v", got)
	}
}

func TestGraphDiagnosticsUseFixedTextAndByteBudget(t *testing.T) {
	secret := "SECRET_like_invalid_identifier"
	findings := []graph.Diagnostic{{Severity: graph.SeverityError, Code: "E_IDENTIFIER_INVALID",
		Message: "identifier " + secret + " is invalid", Suggestion: "remove " + secret,
		Location: graph.DiagnosticLocation{Path: "nodes." + secret}}}
	summary := summarizeDiagnostics(findings)
	if !summary.HasErrors || len(summary.Diagnostics) != 1 ||
		strings.Contains(summary.Diagnostics[0].Message, secret) || strings.Contains(summary.Diagnostics[0].Suggestion, secret) {
		t.Fatalf("unsafe diagnostic text = %#v", summary)
	}
	findings = []graph.Diagnostic{{Severity: graph.SeverityError, Code: "E_IDENTIFIER_INVALID",
		Location: graph.DiagnosticLocation{Path: strings.Repeat("x", maxDiagnosticResponseBytes)}}}
	summary = summarizeDiagnostics(findings)
	encoded, err := json.Marshal(summary)
	if err != nil || len(encoded) > maxDiagnosticResponseBytes || !summary.HasErrors || summary.DiagnosticsOmitted != 1 {
		t.Fatalf("unbounded diagnostic = %d bytes, %#v, %v", len(encoded), summary, err)
	}
}
