package graph

import "testing"

func TestSemanticVersionIgnoresPresentationAndMapInsertionOrder(t *testing.T) {
	first := &Definition{SchemaVersion: SchemaVersionV0, ID: "version-test", Name: "First", Metadata: Metadata{"layout": "left"}, Nodes: map[string]NodeDefinition{"z": {Type: "shell", Config: NodeConfig{"command": "echo", "args": []any{"hi"}}}, "a": {Type: "shell", Config: NodeConfig{"command": "true"}}}, Edges: []EdgeDefinition{{From: "a.outputs.x", To: "z.inputs.x"}, {From: "a.outputs.y", To: "z.inputs.y"}}, GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.x", To: "a.inputs.x"}, {From: "inputs.y", To: "a.inputs.y"}}}
	second := &Definition{SchemaVersion: SchemaVersionV0, ID: "version-test", Name: "Second", Metadata: Metadata{"layout": "right"}, Nodes: map[string]NodeDefinition{"a": {Type: "shell", Config: NodeConfig{"command": "true"}}, "z": {Type: "shell", Config: NodeConfig{"args": []any{"hi"}, "command": "echo"}}}, Edges: []EdgeDefinition{{From: "a.outputs.y", To: "z.inputs.y"}, {From: "a.outputs.x", To: "z.inputs.x"}}, GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.y", To: "a.inputs.y"}, {From: "inputs.x", To: "a.inputs.x"}}}
	left, err := SemanticVersion(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := SemanticVersion(second)
	if err != nil {
		t.Fatal(err)
	}
	if left == "" || left != right {
		t.Fatalf("versions differ: %q, %q", left, right)
	}
	second.Nodes["a"] = NodeDefinition{Type: "shell", Config: NodeConfig{"command": "false"}}
	changed, err := SemanticVersion(second)
	if err != nil {
		t.Fatal(err)
	}
	if changed == left {
		t.Fatalf("semantic change retained version %q", changed)
	}
}
