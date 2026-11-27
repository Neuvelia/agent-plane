package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

// edgeInputGraph is a valid graph whose only edge delivers a.outputs.o to
// b.inputs.i; TARGET is replaced by node b's definition.
const edgeInputGraph = `schema_version: 0
id: edge-input
inputs: {}
nodes:
  a:
    type: shell
    config: {command: /usr/bin/printf, args: [x]}
    outputs:
      o: {type: string}
  b:
TARGET
edges:
  - {from: a.outputs.o, to: b.inputs.i}
outputs: {}
`

// requireNoErrors fails when text has a blocking diagnostic: a "no warning"
// case must not pass only because an error silenced the edge.
func requireNoErrors(t *testing.T, text string) {
	t.Helper()
	if result := validateGraph(parseGraph(t, text)); result.HasErrors() {
		t.Fatalf("unexpected errors: %+v", result.Diagnostics)
	}
}

func edgeInputDiagnostics(t *testing.T, target string) []graph.Diagnostic {
	t.Helper()
	def := parseGraph(t, strings.Replace(edgeInputGraph, "TARGET", target, 1))
	var out []graph.Diagnostic
	for _, d := range validateGraph(def).Diagnostics {
		if d.Code == edgeInputUnusedCode {
			out = append(out, d)
		}
	}
	return out
}

const shellWithoutInputEnv = `    type: shell
    config: {command: /usr/bin/printf, args: [y]}
    inputs:
      i: {type: string, required: true}`

// roadmap: F0.2-AC4
func TestEdgeToAnInputTheShellNodeDoesNotMapWarns(t *testing.T) {
	diags := edgeInputDiagnostics(t, shellWithoutInputEnv)
	edge := 0
	want := graph.Diagnostic{
		Severity:   graph.SeverityWarning,
		Code:       "W_EDGE_INPUT_UNUSED",
		Message:    `node "b" does not use input "i", which edge 0 delivers`,
		GraphID:    "edge-input",
		Location:   graph.DiagnosticLocation{EdgeIndex: &edge, Path: "edges[0].to"},
		Suggestion: `name input "i" in config.input_env so the node reads it; removing the edge would also remove the node's dependency on its source`,
	}
	if len(diags) != 1 || mustJSON(t, diags[0]) != mustJSON(t, want) {
		t.Fatalf("got %+v\nwant %+v", diags, want)
	}
}

// roadmap: F0.2-AC4
func TestUnusedEdgeInputDoesNotBlockValidateOrRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edge-input.yaml")
	text := strings.Replace(edgeInputGraph, "TARGET", shellWithoutInputEnv, 1)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := invoke(t, context.Background(), "graph", "validate", path, "--json")
	if code != 0 || !strings.Contains(stdout, `"W_EDGE_INPUT_UNUSED"`) || !strings.Contains(stdout, `"status":"valid"`) {
		t.Errorf("exit %d, stdout %s", code, stdout)
	}
	if result := validateForRun(parseGraph(t, text)); result.HasErrors() {
		t.Errorf("the warning must not block a run: %+v", result.Diagnostics)
	}
}

// roadmap: F2.3-AC2
func TestUnusedEdgeInputReachesTheEditorAPI(t *testing.T) {
	def := parseGraph(t, strings.Replace(edgeInputGraph, "TARGET", shellWithoutInputEnv, 1))
	found := false
	for _, d := range diagnosticsFor(def) {
		found = found || (d.Code == edgeInputUnusedCode && d.Location.EdgeIndex != nil && *d.Location.EdgeIndex == 0)
	}
	if !found {
		t.Errorf("the document API does not report the warning on edge 0")
	}
}

func TestAgentInputOtherThanTaskWarnsAndNamesTask(t *testing.T) {
	diags := edgeInputDiagnostics(t, `    type: agent
    config: {adapter: codex, workspace: /w, task: do it}
    inputs:
      i: {type: string, required: true}`)
	if len(diags) != 1 || !strings.Contains(diags[0].Suggestion, `reads only input "task"`) {
		t.Errorf("got %+v", diags)
	}
}

// roadmap: F0.2-AC4
func TestInputsTheTargetReadsDoNotWarn(t *testing.T) {
	cases := map[string]string{
		"shell input_env": `    type: shell
    config: {command: /bin/sh, args: ["-c", 'printf "%s" "$I"'], input_env: {i: I}}
    inputs:
      i: {type: string, required: true}`,
		"human context_inputs": `    type: human
    config: {prompt: "ok?", context_inputs: [i]}
    inputs:
      i: {type: string, required: true}
    outputs:
      decision: {type: string, required: true}`,
		"gate command input": `    type: gate
    config: {check_id: same, kind: command, command: /usr/bin/diff, cwd: /tmp, left_input: i, right_input: i}
    inputs:
      i: {type: string, required: true}`,
		// A forwarded input counts only when the embedded Gate reads the Gate
		// input it lands on; validation_test.go covers a Gate that ignores it.
		"loop binding to an input the Gate reads": `    type: shell
    config: {command: /usr/bin/printf, args: ["x"]}
    inputs:
      i: {type: string, required: true}
    outputs:
      stdout: {type: string}
    loop:
      max_attempts: 2
      gate:
        inputs:
          expected: {type: string, required: true}
          candidate: {type: string, required: true}
        config: {check_id: c, kind: command, command: /usr/bin/diff, cwd: /tmp, left_input: expected, right_input: candidate}
      bindings:
        - {from: candidate.outputs.stdout, to: gate.inputs.candidate}
        - {from: node.inputs.i, to: gate.inputs.expected}`,
	}
	for name, target := range cases {
		requireNoErrors(t, strings.Replace(edgeInputGraph, "TARGET", target, 1))
		if diags := edgeInputDiagnostics(t, target); len(diags) != 0 {
			t.Errorf("%s: got %+v", name, diags)
		}
	}
}

func TestAgentTaskInputDoesNotWarn(t *testing.T) {
	text := strings.Replace(edgeInputGraph, "to: b.inputs.i", "to: b.inputs.task", 1)
	text = strings.Replace(text, "TARGET", `    type: agent
    config: {adapter: codex, workspace: /w}
    inputs:
      task: {type: string, required: true}`, 1)
	requireNoErrors(t, text)
	for _, d := range validateGraph(parseGraph(t, text)).Diagnostics {
		if d.Code == edgeInputUnusedCode {
			t.Errorf("got %+v", d)
		}
	}
}

// ADR-0017: a node waits for approval through a data edge from the human
// node's decision, whether or not it reads the value.
func TestHumanDecisionEdgeIsAnApprovalGateAndDoesNotWarn(t *testing.T) {
	text := strings.Replace(edgeInputGraph, `  a:
    type: shell
    config: {command: /usr/bin/printf, args: [x]}
    outputs:
      o: {type: string}`, `  a:
    type: human
    config: {prompt: "go?"}
    outputs:
      decision: {type: string, required: true}`, 1)
	text = strings.Replace(text, "a.outputs.o", "a.outputs.decision", 1)
	text = strings.Replace(text, "TARGET", shellWithoutInputEnv, 1)
	requireNoErrors(t, text)
	for _, d := range validateGraph(parseGraph(t, text)).Diagnostics {
		if d.Code == edgeInputUnusedCode {
			t.Errorf("got %+v", d)
		}
	}
}

func TestEdgeWithABlockingErrorIsNotAlsoWarned(t *testing.T) {
	text := strings.Replace(edgeInputGraph, "TARGET", strings.Replace(shellWithoutInputEnv, "i: {type: string", "i: {type: number", 1), 1)
	result := validateGraph(parseGraph(t, text))
	if !result.HasErrors() {
		t.Fatalf("want a type mismatch: %+v", result.Diagnostics)
	}
	for _, d := range result.Diagnostics {
		if d.Code == edgeInputUnusedCode {
			t.Errorf("an invalid edge is also warned: %+v", d)
		}
	}
}

// input_env accepts string inputs only, so the suggestion must not offer it for
// a json input: following it would turn the warning into a config error.
func TestSuggestionOffersOnlyKeysThatAcceptTheInputType(t *testing.T) {
	text := strings.Replace(edgeInputGraph, "TARGET", strings.Replace(shellWithoutInputEnv, "i: {type: string", "i: {type: json", 1), 1)
	text = strings.Replace(text, "config: {command: /usr/bin/printf, args: [x]}", "config: {command: /usr/bin/printf, args: [\"{}\"], stdout_json: true}", 1)
	text = strings.Replace(text, "o: {type: string}", "o: {type: json}", 1)
	text = strings.Replace(text, "a.outputs.o", "a.outputs.json", 1)
	text = strings.Replace(text, "      o: {type: json}", "      json: {type: json}", 1)
	requireNoErrors(t, text)
	var diags []graph.Diagnostic
	for _, d := range validateGraph(parseGraph(t, text)).Diagnostics {
		if d.Code == edgeInputUnusedCode {
			diags = append(diags, d)
		}
	}
	if len(diags) != 1 || strings.Contains(diags[0].Suggestion, "input_env") || !strings.Contains(diags[0].Suggestion, "no input of this name or type") {
		t.Errorf("got %+v", diags)
	}
}

// ADR-0017: an edge into a human node makes the approval wait for its source,
// even when the person is not shown the value (a json input).
func TestEdgeIntoAHumanNodeIsAnApprovalDependencyAndDoesNotWarn(t *testing.T) {
	text := strings.Replace(edgeInputGraph, "TARGET", `    type: human
    config: {prompt: "ok?"}
    inputs:
      i: {type: string, required: true}
    outputs:
      decision: {type: string, required: true}`, 1)
	requireNoErrors(t, text)
	for _, d := range validateGraph(parseGraph(t, text)).Diagnostics {
		if d.Code == edgeInputUnusedCode {
			t.Errorf("got %+v", d)
		}
	}
}

// roadmap: F0.2-AC4
func TestShippedGraphsHaveNoUnusedEdgeInput(t *testing.T) {
	paths, _ := filepath.Glob("../../.agents/graphs/*.yaml")
	templates, _ := filepath.Glob("../../.agents/templates/*.yaml")
	for _, p := range append(paths, templates...) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range validateGraph(parseGraph(t, string(data))).Diagnostics {
			if d.Code == edgeInputUnusedCode {
				t.Errorf("%s: %s", p, d.Message)
			}
		}
	}
}
