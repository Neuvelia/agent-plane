package yamlgraph_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"strings"
	"testing"
)

func TestUnitsParsingAndValidation(t *testing.T) {
	base := `schema_version: 0
id: units
nodes: {}
inputs: {}
edges: []
outputs: {}
`
	for _, field := range []string{"units: null", "units: []", "units: [1]", "units: [true]", "units: [null]", "units: {}"} {
		if _, e := yamlgraph.Parse([]byte(base + field + "\n")); e == nil {
			t.Fatalf("accepted %s", field)
		}
	}
	for _, units := range [][]string{{}, {""}, {"unknown"}, {"a", "a"}, {"shell"}, {"plain"}} {
		d := unitGraph()
		d.Units = units
		if graph.Validate(d).Valid() {
			t.Fatalf("accepted %#v", units)
		}
	}
	d := unitGraph()
	d.Units = []string{"b", "a"}
	if r := graph.Validate(d); !r.Valid() {
		t.Fatal(r)
	}
	b, e := yamlgraph.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	round, e := yamlgraph.Parse(b)
	if e != nil || len(round.Units) != 2 {
		t.Fatal(e, string(b))
	}
}
func unitGraph() *graph.Definition {
	n := graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"allowed_paths": []string{}}, Outputs: map[string]graph.PortDefinition{"raw_metadata": {Type: graph.ValueTypeJSON}}, Loop: &graph.LoopDefinition{Gate: graph.LoopGateDefinition{Config: graph.NodeConfig{"kind": "json_predicate"}, Inputs: map[string]graph.PortDefinition{"x": {Type: graph.ValueTypeJSON, Required: true}}}, Bindings: []graph.LoopBinding{{From: "candidate.outputs.raw_metadata", To: "gate.inputs.x"}}}}
	return &graph.Definition{ID: "units", Inputs: map[string]graph.InputDefinition{}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}, Nodes: map[string]graph.NodeDefinition{"a": n, "b": n, "shell": {Type: "shell"}, "plain": {Type: "agent"}}}
}
func TestUnitSetHashAndAbsentBytes(t *testing.T) {
	d := unitGraph()
	before, e := yamlgraph.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(before), "units:") {
		t.Fatal("legacy bytes changed")
	}
	legacy, e := yamlgraph.SemanticVersion(d)
	if e != nil {
		t.Fatal(e)
	}
	d.Units = []string{"b", "a"}
	v, e := yamlgraph.SemanticVersion(d)
	if e != nil {
		t.Fatal(e)
	}
	d.Units = []string{"a", "b"}
	w, e := yamlgraph.SemanticVersion(d)
	if e != nil || v != w || v == legacy {
		t.Fatal(v, w, legacy, e)
	}
	if d.Units[0] != "a" {
		t.Fatal("hash mutated author membership")
	}
}
