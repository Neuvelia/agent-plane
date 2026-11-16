package edit

import (
	"testing"

	"agentplane/internal/domain/graph"
)

const linesGraph = `# header
schema_version: 0
id: g
inputs:
  topic: {type: string}
nodes:
  a:
    type: shell
    outputs:
      out: {type: string}
  b:
    type: shell
    inputs:
      in: {type: string}
edges:
  - {from: a.outputs.out, to: b.inputs.in}
  - {from: a.outputs.out, to: b.inputs.nope}
graph_input_bindings:
  - {from: inputs.topic, to: b.inputs.in}
outputs: {}
units: [a]
`

func idx(n int) *int { return &n }

func TestDiagnosticLinesResolveFromThePathThenIndexThenNode(t *testing.T) {
	diags := []graph.Diagnostic{
		{Code: "path-deep", Location: graph.DiagnosticLocation{NodeID: "a", Path: "nodes.a.outputs.out"}},
		{Code: "path-missing-leaf", Location: graph.DiagnosticLocation{NodeID: "a", Path: "nodes.a.outputs.missing"}}, // falls back to nodes.a.outputs
		{Code: "edge-index", Location: graph.DiagnosticLocation{EdgeIndex: idx(1)}},
		{Code: "edge-path", Location: graph.DiagnosticLocation{EdgeIndex: idx(1), Path: "edges[1].to"}},
		{Code: "binding", Location: graph.DiagnosticLocation{GraphInputBindingIndex: idx(0)}},
		{Code: "node-only", Location: graph.DiagnosticLocation{NodeID: "b"}},
		{Code: "unit", Location: graph.DiagnosticLocation{Path: "units[0]"}},
		{Code: "graph-level"},
		{Code: "gone", Location: graph.DiagnosticLocation{NodeID: "zzz", Path: "nodes.zzz.config"}},
	}
	got := DiagnosticLines([]byte(linesGraph), diags)
	want := []int{10, 9, 17, 17, 19, 11, 21, 0, 6} // a missing node resolves to the nearest part that exists: the nodes block
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: line %d, want %d", diags[i].Code, got[i], want[i])
		}
	}
}

func TestDiagnosticLinesNeverFail(t *testing.T) {
	got := DiagnosticLines([]byte("schema_version: 0\nid: ["), []graph.Diagnostic{{Code: "x", Location: graph.DiagnosticLocation{Path: "id"}}})
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("a text that does not parse has no lines: %v", got)
	}
	if len(DiagnosticLines([]byte(linesGraph), nil)) != 0 {
		t.Error("no diagnostics, no lines")
	}
}

func TestParseErrorLine(t *testing.T) {
	for msg, want := range map[string]int{
		"parse graph definition: yaml: line 3: did not find expected node content":                              3,
		"parse graph definition: yaml: unmarshal errors:\n  line 7: field x not found in type graph.Definition": 7,
		"parse graph definition: schema_version is required":                                                    0,
	} {
		if got := ParseErrorLine(msg); got != want {
			t.Errorf("%q: %d, want %d", msg, got, want)
		}
	}
}
