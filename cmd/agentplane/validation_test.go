package main

import (
	"agentplane/internal/adapter/yamlgraph"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

const shellNoCommand = `schema_version: 0
id: broken
inputs: {}
nodes:
  work:
    type: shell
    config:
      command: ""
    outputs:
      stdout: {type: string}
edges: []
outputs:
  result: {from: work.outputs.stdout}
`

func parseGraph(t *testing.T, text string) *graph.Definition {
	t.Helper()
	def, err := yamlgraph.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func configDiagnostics(diags []graph.Diagnostic) []graph.Diagnostic {
	var out []graph.Diagnostic
	for _, d := range diags {
		if d.Code == nodeConfigCode {
			out = append(out, d)
		}
	}
	return out
}

// The runnable graphs the project ships must stay valid under the stricter
// path. docs/examples are templates with placeholder workspaces that an author
// must edit, so they are expected to report configuration errors until then.
func TestRunnableGraphsHaveNoConfigErrors(t *testing.T) {
	paths, _ := filepath.Glob("../../.agents/graphs/*.yaml")
	if len(paths) < 3 {
		t.Fatalf("found only %d graphs", len(paths))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		def, err := yamlgraph.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if bad := configDiagnostics(validateGraph(def).Diagnostics); len(bad) > 0 {
			t.Errorf("%s: unexpected config errors: %+v", p, bad)
		}
	}
}

func TestTemplateExamplesReportTheirPlaceholders(t *testing.T) {
	data, err := os.ReadFile("../../docs/examples/scoped-retry.yaml")
	if err != nil {
		t.Fatal(err)
	}
	def, err := yamlgraph.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(configDiagnostics(validateGraph(def).Diagnostics)) == 0 {
		t.Errorf("a template with placeholder workspaces should report configuration errors")
	}
}

// roadmap: F0.2-AC2
func TestConfigErrorsBecomeDiagnosticsWithTheNodeAndPath(t *testing.T) {
	diags := configDiagnostics(validateGraph(parseGraph(t, shellNoCommand)).Diagnostics)
	if len(diags) != 1 {
		t.Fatalf("got %+v", diags)
	}
	d := diags[0]
	if d.Severity != graph.SeverityError || d.Location.NodeID != "work" || d.Location.Path != "nodes.work.config" || d.GraphID != "broken" || d.Message == "" || d.Suggestion == "" {
		t.Errorf("diagnostic %+v", d)
	}
}

const unknownNodeType = `schema_version: 0
id: unknown-type
inputs: {}
nodes:
  work:
    type: foo
edges: []
outputs: {}
`

// roadmap: F0.1-AC1
func TestUnknownNodeTypeIsABlockingDiagnosticWithTheNodeAndPath(t *testing.T) {
	result := validateGraph(parseGraph(t, unknownNodeType))
	if !result.HasErrors() || len(result.Diagnostics) != 1 {
		t.Fatalf("got %+v", result.Diagnostics)
	}
	d := result.Diagnostics[0]
	want := graph.Diagnostic{
		Severity:   graph.SeverityError,
		Code:       "E_NODE_TYPE_UNKNOWN",
		Message:    `node "work" has unsupported type "foo"`,
		GraphID:    "unknown-type",
		Location:   graph.DiagnosticLocation{NodeID: "work", Path: "nodes.work.type"},
		Suggestion: "use one of the node types: agent, gate, human, shell",
	}
	if d != want {
		t.Errorf("diagnostic\n got %+v\nwant %+v", d, want)
	}
}

func TestUnknownNodeTypeTextOutsideTheGrammarIsNotEchoed(t *testing.T) {
	text := strings.Replace(unknownNodeType, "type: foo", "type: \"sk-live SECRET\"", 1)
	diags := validateGraph(parseGraph(t, text)).Diagnostics
	if len(diags) != 1 || strings.Contains(diags[0].Message, "SECRET") || diags[0].Location.Path != "nodes.work.type" {
		t.Errorf("got %+v", diags)
	}
}

func TestEveryCatalogueTypeIsKnown(t *testing.T) {
	for _, nodeType := range []string{"agent", "gate", "human", "shell"} {
		text := strings.Replace(unknownNodeType, "type: foo", "type: "+nodeType, 1)
		for _, d := range validateGraph(parseGraph(t, text)).Diagnostics {
			if d.Code == "E_NODE_TYPE_UNKNOWN" {
				t.Errorf("%s reported as unknown: %+v", nodeType, d)
			}
		}
	}
}

// roadmap: F0.2-AC1
func TestUnknownNodeTypeExitsWithTheGraphCodeBeforeAnyRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.yaml")
	if err := os.WriteFile(path, []byte(unknownNodeType), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"graph", "validate", path, "--json"}, {"run", path, "--json"}} {
		code, stdout, _ := invoke(t, context.Background(), args...)
		if code != exitGraph || !strings.Contains(stdout, "E_NODE_TYPE_UNKNOWN") || strings.Contains(stdout, "run_started") {
			t.Errorf("%v: exit %d, stdout %s", args, code, stdout)
		}
	}
}

func TestLoopGateConfigErrorsPointAtTheGate(t *testing.T) {
	text := strings.Replace(shellNoCommand, "command: \"\"", "command: /bin/true", 1)
	text = strings.Replace(text, "      stdout: {type: string}\nedges", `      stdout: {type: string}
      json: {type: json}
    loop:
      max_attempts: 2
      gate:
        inputs:
          candidate: {type: json, required: true}
        config:
          check_id: ""
          kind: json_predicate
          input: candidate
          field: [ok]
          operator: eq
          expected: true
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.candidate}
edges`, 1)
	text = strings.Replace(text, "      command: /bin/true", "      command: /bin/true\n      stdout_json: true", 1)
	diags := configDiagnostics(validateGraph(parseGraph(t, text)).Diagnostics)
	if len(diags) != 1 || diags[0].Location.Path != "nodes.work.loop.gate" {
		t.Errorf("got %+v", diags)
	}
}

func TestOrderIsGraphRulesThenConfigByNodeID(t *testing.T) {
	text := `schema_version: 0
id: order
inputs: {}
nodes:
  b:
    type: shell
    config: {command: ""}
  a:
    type: shell
    config: {command: ""}
edges: []
outputs: {}
`
	diags := configDiagnostics(validateGraph(parseGraph(t, text)).Diagnostics)
	if len(diags) != 2 || diags[0].Location.NodeID != "a" || diags[1].Location.NodeID != "b" {
		t.Errorf("config diagnostics are not in node id order: %+v", diags)
	}
	if again := configDiagnostics(validateGraph(parseGraph(t, text)).Diagnostics); len(again) != 2 || again[0] != diags[0] {
		t.Errorf("not deterministic")
	}
}

// The CLI, `serve`'s file validation and the document API report the same thing
// (ADR-0021: every CLI diagnostic has an API counterpart).
func TestCLIAndAPIReportTheSameDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(path, []byte(shellNoCommand), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := invoke(t, context.Background(), "graph", "validate", path, "--json")
	if code != exitGraph {
		t.Fatalf("exit %d, want %d", code, exitGraph)
	}
	var cli struct {
		Diagnostics []graph.Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &cli); err != nil {
		t.Fatalf("CLI output is not JSON: %v\n%s", err, stdout)
	}
	if len(configDiagnostics(cli.Diagnostics)) != 1 {
		t.Fatalf("the CLI did not report the config error: %+v", cli.Diagnostics)
	}
	status, api, err := validateForAPI(path)
	if err != nil || status != "invalid" {
		t.Fatalf("API file validation: %q %v", status, err)
	}
	if want, got := mustJSON(t, cli.Diagnostics), mustJSON(t, api); want != got {
		t.Errorf("CLI and API differ:\nCLI %s\nAPI %s", want, got)
	}
	if got := mustJSON(t, diagnosticsFor(parseGraph(t, shellNoCommand))); got != mustJSON(t, api) {
		t.Errorf("document validation differs from file validation:\n%s\n%s", got, mustJSON(t, api))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
