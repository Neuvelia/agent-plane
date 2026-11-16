package edit

import (
	"testing"

	"agentplane/internal/adapter/yamlgraph"
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

func TestParseErrorColumnComesFromTheFailingNodeNotFromTheMessage(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		line int
		col  int
	}{
		"syntax error: the library names no column":    {"schema_version: 0\nid: [\nnodes: {}\n", 1, 0},
		"unknown top-level field":                      {"schema_version: 0\nid: g\nnodes: {}\nbogus: 1\n", 4, 1},
		"unknown nested field":                         {"schema_version: 0\nid: g\nnodes:\n  a:\n    type: shell\n    colour: red\n", 6, 5},
		"wrong kind (sequence for a string)":           {"schema_version: 0\nid: [a]\n", 2, 5},
		"wrong type (int for a mapping)":               {"schema_version: 0\nid: g\ninputs: 5\n", 3, 9},
		"long scalar is matched by its shortened text": {"schema_version: 0\nid: g\ninputs: 1234567890123\n", 3, 9},
		"two candidates on one line stay unlocated":    {"schema_version: 0\nid: g\nnodes: {a: {colour: 1}, b: {colour: 2}}\n", 3, 0},
		"column counts characters, not bytes":          {"schema_version: 0\nid: g\nnodes:\n  zażółć: {colour: 1}\n", 4, 12},
	} {
		_, err := yamlgraph.Parse([]byte(tc.text))
		if err == nil {
			t.Fatalf("%s: text unexpectedly parses", name)
		}
		if got := ParseErrorLine(err.Error()); got != tc.line {
			t.Errorf("%s: line %d, want %d (%v)", name, got, tc.line, err)
		}
		if got := ParseErrorColumn([]byte(tc.text), err.Error()); got != tc.col {
			t.Errorf("%s: column %d, want %d (%v)", name, got, tc.col, err)
		}
	}
}

func TestParseErrorColumnIsZeroWhenThereIsNothingToLocate(t *testing.T) {
	if got := ParseErrorColumn([]byte("a: 1"), "parse graph definition: schema_version is required"); got != 0 {
		t.Errorf("a message without a line has no column: %d", got)
	}
	if got := ParseErrorColumn([]byte("id: ["), "yaml: line 1: field id not found in type x"); got != 0 {
		t.Errorf("text that does not parse has no tree: %d", got)
	}
}
