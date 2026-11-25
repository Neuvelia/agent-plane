package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

// pastedSecret stands for author text typed where a key or reference belongs.
// It fails the identifier grammar, as a pasted credential usually does.
const pastedSecret = "TOKEN=sk_Live9f3Q"

func authorTextBaseDefinition() *Definition {
	return &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "g",
		Inputs:        map[string]InputDefinition{"text": {Type: ValueTypeString}},
		Nodes: map[string]NodeDefinition{
			"a": {Type: "shell", Config: NodeConfig{"command": "/bin/true"}, Outputs: map[string]PortDefinition{"o": {Type: ValueTypeString}}},
			"b": {Type: "shell", Config: NodeConfig{"command": "/bin/true"}, Inputs: map[string]PortDefinition{"i": {Type: ValueTypeString}}},
		},
		Edges:   []EdgeDefinition{{From: "a.outputs.o", To: "b.inputs.i"}},
		Outputs: map[string]OutputDefinition{},
	}
}

func TestDiagnosticsNeverQuoteInvalidAuthorText(t *testing.T) {
	s := pastedSecret
	for _, tc := range []struct {
		name   string
		mutate func(*Definition)
	}{
		{"graph id", func(d *Definition) { d.ID = s }},
		{"graph input key", func(d *Definition) { d.Inputs[s] = InputDefinition{Type: ValueTypeString} }},
		{"graph input type", func(d *Definition) { d.Inputs["text"] = InputDefinition{Type: ValueType(s)} }},
		{"node id without type", func(d *Definition) { d.Nodes[s] = NodeDefinition{} }},
		{"node input key", func(d *Definition) {
			d.Nodes["b"].Inputs[s] = PortDefinition{Type: ValueTypeString, Required: true}
		}},
		{"node output key", func(d *Definition) { d.Nodes["a"].Outputs[s] = PortDefinition{Type: ValueTypeString} }},
		{"edge source", func(d *Definition) { d.Edges[0].From = NodeOutputReference(s) }},
		{"edge target", func(d *Definition) { d.Edges[0].To = NodeInputReference(s) }},
		{"graph input binding", func(d *Definition) {
			d.GraphInputBindings = []GraphInputBindingDefinition{{From: GraphInputReference(s), To: NodeInputReference(s)}}
		}},
		{"graph output key", func(d *Definition) { d.Outputs[s] = OutputDefinition{From: "ghost.outputs.o"} }},
		{"graph output source", func(d *Definition) { d.Outputs["r"] = OutputDefinition{From: NodeOutputReference(s)} }},
		{"unit", func(d *Definition) { d.Units = []string{s} }},
		{"human config key and context input", func(d *Definition) {
			d.Nodes["h"] = NodeDefinition{Type: "human", Config: NodeConfig{s: "x", "context_inputs": []string{s}}, Outputs: map[string]PortDefinition{"decision": {Type: ValueTypeString}}}
		}},
		{"human node id with timeout", func(d *Definition) {
			seconds := 5
			d.Nodes[s] = NodeDefinition{Type: "human", TimeoutSeconds: &seconds, Outputs: map[string]PortDefinition{"decision": {Type: ValueTypeString}}}
		}},
		{"agent node id with secret_env", func(d *Definition) {
			d.Nodes[s] = NodeDefinition{Type: "agent", Config: NodeConfig{secretEnvKey: map[string]string{"T": "t"}}}
		}},
		{"loop gate input key", func(d *Definition) {
			node := d.Nodes["a"]
			node.Loop = &LoopDefinition{MaxAttempts: 2, Gate: LoopGateDefinition{Inputs: map[string]PortDefinition{s: {Type: ValueTypeString}}, Config: NodeConfig{"kind": "x"}}}
			d.Nodes["a"] = node
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := authorTextBaseDefinition()
			tc.mutate(definition)
			result := Validate(definition)
			if !result.HasErrors() {
				t.Fatalf("expected an error, got %#v", result.Diagnostics)
			}
			encoded, err := json.Marshal(result.Diagnostics)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "sk_Live9f3Q") {
				t.Fatalf("diagnostics echo author text: %s", encoded)
			}
		})
	}
}

func TestInvalidNodeIDDiagnosticsPointAtNodesCollection(t *testing.T) {
	definition := authorTextBaseDefinition()
	definition.Nodes[pastedSecret] = NodeDefinition{Inputs: map[string]PortDefinition{"x": {Type: ValueTypeString, Required: true}}}
	for _, diagnostic := range Validate(definition).Diagnostics {
		if diagnostic.Location.NodeID == "" && diagnostic.Location.Path != "nodes" && strings.HasPrefix(diagnostic.Location.Path, "nodes") {
			t.Errorf("%s located at %q, want the nodes collection", diagnostic.Code, diagnostic.Location.Path)
		}
	}
}

func TestUnknownSuppliedInputIsCountedWithoutItsKey(t *testing.T) {
	plan, compiled := CompileBindings(authorTextBaseDefinition())
	if !compiled.Valid() {
		t.Fatalf("compile: %#v", compiled.Diagnostics)
	}
	// A valid-looking key is not echoed either: it comes from --input or the
	// API, not from the graph.
	result := plan.ValidateGraphInputs(map[string]Value{"leakedsecret9": NewStringValue("x"), pastedSecret: NewStringValue("y")})
	encoded, err := json.Marshal(result.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "leakedsecret9") || strings.Contains(string(encoded), "sk_Live9f3Q") {
		t.Fatalf("diagnostics echo supplied keys: %s", encoded)
	}
	if len(result.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want one per undeclared key", result.Diagnostics)
	}
	for i, diagnostic := range result.Diagnostics {
		want := []string{"(1 of 2)", "(2 of 2)"}[i]
		if diagnostic.Code != "E_GRAPH_INPUT_VALUE_UNKNOWN" || diagnostic.Location.Path != "inputs" || !strings.Contains(diagnostic.Message, want) {
			t.Errorf("diagnostic %d = %#v", i, diagnostic)
		}
		if !strings.Contains(diagnostic.Suggestion, "declared inputs: text") {
			t.Errorf("suggestion %q does not list the declared inputs", diagnostic.Suggestion)
		}
	}
}
