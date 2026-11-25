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
		{"node type with secret_env", func(d *Definition) {
			d.Nodes["t"] = NodeDefinition{Type: s, Config: NodeConfig{secretEnvKey: map[string]string{"T": "t"}}}
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
	seconds := 0
	definition.Nodes[pastedSecret] = NodeDefinition{
		Type:           "shell",
		TimeoutSeconds: &seconds,
		Inputs:         map[string]PortDefinition{"x": {Type: ValueTypeString, Required: true}},
		Outputs:        map[string]PortDefinition{"y": {Type: ValueTypeString}},
	}
	diagnostics := Validate(definition).Diagnostics
	if len(diagnostics) < 4 {
		t.Fatalf("diagnostics = %#v, want identifier, timeout, unbound input and unused output", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		location := diagnostic.Location
		if location.NodeID == pastedSecret {
			t.Errorf("%s carries the invalid node id", diagnostic.Code)
		}
		inValidNode := strings.HasPrefix(location.Path, "nodes.a.") || strings.HasPrefix(location.Path, "nodes.b.")
		if strings.HasPrefix(location.Path, "nodes") && location.Path != "nodes" && !inValidNode {
			t.Errorf("%s located at %q, want the nodes collection", diagnostic.Code, location.Path)
		}
		if diagnostic.Code == "E_TIMEOUT_INVALID" && !strings.HasPrefix(diagnostic.Message, "timeout_seconds is 0") {
			t.Errorf("timeout message %q does not name the field", diagnostic.Message)
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
