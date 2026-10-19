package graph

import (
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
		if _, e := Parse([]byte(base + field + "\n")); e == nil {
			t.Fatalf("accepted %s", field)
		}
	}
	for _, units := range [][]string{{}, {""}, {"unknown"}, {"a", "a"}, {"shell"}, {"plain"}} {
		d := unitGraph()
		d.Units = units
		if Validate(d).Valid() {
			t.Fatalf("accepted %#v", units)
		}
	}
	d := unitGraph()
	d.Units = []string{"b", "a"}
	if r := Validate(d); !r.Valid() {
		t.Fatal(r)
	}
	b, e := Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	round, e := Parse(b)
	if e != nil || len(round.Units) != 2 {
		t.Fatal(e, string(b))
	}
}
func unitGraph() *Definition {
	n := NodeDefinition{Type: "agent", Config: NodeConfig{"allowed_paths": []string{}}, Outputs: map[string]PortDefinition{"raw_metadata": {Type: ValueTypeJSON}}, Loop: &LoopDefinition{Gate: LoopGateDefinition{Config: NodeConfig{"kind": "json_predicate"}, Inputs: map[string]PortDefinition{"x": {Type: ValueTypeJSON, Required: true}}}, Bindings: []LoopBinding{{From: "candidate.outputs.raw_metadata", To: "gate.inputs.x"}}}}
	return &Definition{ID: "units", Inputs: map[string]InputDefinition{}, Edges: []EdgeDefinition{}, Outputs: map[string]OutputDefinition{}, Nodes: map[string]NodeDefinition{"a": n, "b": n, "shell": {Type: "shell"}, "plain": {Type: "agent"}}}
}
func TestUnitSetHashAndAbsentBytes(t *testing.T) {
	d := unitGraph()
	before, e := Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(before), "units:") {
		t.Fatal("legacy bytes changed")
	}
	legacy, e := SemanticVersion(d)
	if e != nil {
		t.Fatal(e)
	}
	d.Units = []string{"b", "a"}
	v, e := SemanticVersion(d)
	if e != nil {
		t.Fatal(e)
	}
	d.Units = []string{"a", "b"}
	w, e := SemanticVersion(d)
	if e != nil || v != w || v == legacy {
		t.Fatal(v, w, legacy, e)
	}
	if d.Units[0] != "a" {
		t.Fatal("hash mutated author membership")
	}
}
