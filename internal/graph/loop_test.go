package graph

import (
	"strings"
	"testing"
)

func TestLoopBudgetParsing(t *testing.T) {
	for _, value := range []string{"null", "0", "-1", "11", "1.5", "\"2\""} {
		_, err := Parse([]byte("schema_version: 0\nnodes:\n  work:\n    type: agent\n    loop:\n      max_attempts: " + value + "\n"))
		if err == nil {
			t.Errorf("accepted budget %s", value)
		}
	}
	for _, value := range []string{"", "max_attempts: 1", "max_attempts: 10"} {
		d, err := Parse([]byte("schema_version: 0\nnodes:\n  work:\n    type: agent\n    loop: {" + value + "}\n"))
		if err != nil {
			t.Fatal(err)
		}
		if d.Nodes["work"].Loop.Attempts() < 1 {
			t.Fatal("missing budget")
		}
	}
	for _, field := range []string{"loop: {}", "type: gate", "outputs: {}", "unknown: true"} {
		_, err := Parse([]byte("schema_version: 0\nnodes:\n  work:\n    type: agent\n    loop:\n      gate:\n        " + field + "\n"))
		if err == nil {
			t.Errorf("accepted embedded gate %s", field)
		}
	}
}
func TestLoopNilRoundTrip(t *testing.T) {
	d := &Definition{SchemaVersion: 0, Nodes: map[string]NodeDefinition{"work": {Type: "agent"}}}
	b, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "loop") {
		t.Fatal(string(b))
	}
	d2, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := SemanticVersion(d)
	c, _ := SemanticVersion(d2)
	if a != c {
		t.Fatal(a, c)
	}
}
func TestLoopValidationBindings(t *testing.T) {
	base := `schema_version: 0
id: loop
inputs: {}
nodes:
  work:
    type: agent
    config: {allowed_paths: []}
    outputs: {candidate: {type: json}}
    loop:
      gate:
        inputs: {value: {type: json, required: true}}
        config: {check_id: check, kind: json_predicate}
      bindings: [{from: candidate.outputs.candidate, to: gate.inputs.value}]
edges: []
outputs: {}
`
	for _, tt := range []struct{ name, old, new string }{
		{"valid", "none", "none"},
		{"no-candidate", "candidate.outputs.candidate", "node.inputs.candidate"},
		{"wrong-source", "candidate.outputs.candidate", "other.outputs.candidate"},
		{"unknown-port", "candidate.outputs.candidate", "candidate.outputs.unknown"},
		{"wrong-target", "gate.inputs.value", "work.inputs.value"},
		{"type-mismatch", "value: {type: json", "value: {type: string"},
		{"missing-required", "bindings: [{from: candidate.outputs.candidate, to: gate.inputs.value}]", "bindings: []"},
		{"duplicate", "bindings: [{from: candidate.outputs.candidate, to: gate.inputs.value}]", "bindings: [{from: candidate.outputs.candidate, to: gate.inputs.value}, {from: candidate.outputs.candidate, to: gate.inputs.value}]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := Parse([]byte(strings.Replace(base, tt.old, tt.new, 1)))
			if err != nil {
				t.Fatal(err)
			}
			if Validate(d).Valid() != (tt.name == "valid") {
				t.Fatal(Validate(d))
			}
		})
	}
}
func TestOpenMetadataUnchangedByLoopParsing(t *testing.T) {
	_, err := Parse([]byte("schema_version: 0\nmetadata: {loop: null, max_attempts: 0}\nnodes: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
}
func TestNilLoopPreservesFrozenSchemaZeroBytes(t *testing.T) {
	// Frozen pre-F1.6 schema-v0 encoding; omitted loop must add no bytes.
	d := &Definition{SchemaVersion: 0, ID: "stable", Inputs: map[string]InputDefinition{}, Nodes: map[string]NodeDefinition{"work": {Type: "shell", Config: NodeConfig{"command": "true"}}}, Edges: []EdgeDefinition{}, Outputs: map[string]OutputDefinition{}}
	b, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	frozen := "schema_version: 0\nid: stable\ninputs: {}\nnodes:\n    work:\n        type: shell\n        config:\n            command: \"true\"\nedges: []\noutputs: {}\n"
	if string(b) != frozen {
		t.Fatalf("nil-loop encoding changed: %s", b)
	}
}
func TestLoopMergedBudgetPresence(t *testing.T) {
	for _, document := range []string{
		"schema_version: 0\nmetadata: &defaults {max_attempts: 0}\nnodes: {work: {type: agent, loop: {<<: *defaults}}}",
		"schema_version: 0\nmetadata: &defaults {max_attempts: null}\nnodes: {work: {type: agent, loop: {<<: *defaults}}}",
		"schema_version: 0\nmetadata: &nodes {work: {type: agent, loop: {max_attempts: 0}}}\nnodes: {<<: *nodes}",
	} {
		if _, err := Parse([]byte(document)); err == nil {
			t.Fatal("merged invalid budget accepted")
		}
	}
}

func TestLoopProducerTypes(t *testing.T) {
	doc := func(typ, config string) *Definition {
		d, err := Parse([]byte("schema_version: 0\nid: l\ninputs: {}\nnodes:\n  work:\n    type: " + typ + "\n    config: " + config + "\n    outputs: {out: {type: json}}\n    loop:\n      gate:\n        inputs: {v: {type: json, required: true}}\n        config: {check_id: c, kind: json_predicate}\n      bindings: [{from: candidate.outputs.out, to: gate.inputs.v}]\nedges: []\noutputs: {}\n"))
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for _, tt := range []struct {
		name string
		d    *Definition
		ok   bool
	}{
		{"agent with allowed_paths", doc("agent", "{allowed_paths: []}"), true},
		{"agent without allowed_paths", doc("agent", "{}"), false},
		{"shell without allowed_paths", doc("shell", "{}"), true},
		{"gate producer", doc("gate", "{}"), false},
		{"human producer", doc("human", "{}"), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Validate(tt.d).Valid(); got != tt.ok {
				t.Fatalf("valid=%v want %v: %v", got, tt.ok, Validate(tt.d))
			}
		})
	}
}

func TestShellLoopRejectsChangedFilesScopeGate(t *testing.T) {
	d, err := Parse([]byte("schema_version: 0\nid: l\ninputs: {}\nnodes:\n  work:\n    type: shell\n    outputs: {out: {type: json}}\n    loop:\n      gate:\n        inputs: {v: {type: json, required: true}}\n        config: {check_id: c, kind: changed_files_scope}\n      bindings: [{from: candidate.outputs.out, to: gate.inputs.v}]\nedges: []\noutputs: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if Validate(d).Valid() {
		t.Fatal("changed_files_scope gate must require an Agent producer")
	}
}
