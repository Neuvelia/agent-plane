package yamlgraph_test

import (
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
)

func TestSemanticVersionIgnoresPresentationAndMapInsertionOrder(t *testing.T) {
	first := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "version-test", Name: "First", Metadata: graph.Metadata{"layout": "left"}, Nodes: map[string]graph.NodeDefinition{"z": {Type: "shell", Config: graph.NodeConfig{"command": "echo", "args": []any{"hi"}}}, "a": {Type: "shell", Config: graph.NodeConfig{"command": "true"}}}, Edges: []graph.EdgeDefinition{{From: "a.outputs.x", To: "z.inputs.x"}, {From: "a.outputs.y", To: "z.inputs.y"}}, GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.x", To: "a.inputs.x"}, {From: "inputs.y", To: "a.inputs.y"}}}
	second := &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "version-test", Name: "Second", Metadata: graph.Metadata{"layout": "right"}, Nodes: map[string]graph.NodeDefinition{"a": {Type: "shell", Config: graph.NodeConfig{"command": "true"}}, "z": {Type: "shell", Config: graph.NodeConfig{"args": []any{"hi"}, "command": "echo"}}}, Edges: []graph.EdgeDefinition{{From: "a.outputs.y", To: "z.inputs.y"}, {From: "a.outputs.x", To: "z.inputs.x"}}, GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.y", To: "a.inputs.y"}, {From: "inputs.x", To: "a.inputs.x"}}}
	left, err := yamlgraph.SemanticVersion(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := yamlgraph.SemanticVersion(second)
	if err != nil {
		t.Fatal(err)
	}
	if left == "" || left != right {
		t.Fatalf("versions differ: %q, %q", left, right)
	}
	second.Nodes["a"] = graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "false"}}
	changed, err := yamlgraph.SemanticVersion(second)
	if err != nil {
		t.Fatal(err)
	}
	if changed == left {
		t.Fatalf("semantic change retained version %q", changed)
	}
}
