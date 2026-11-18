package yamlgraph_test

import (
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
)

// pinnedDefinition has no limits; its version was recorded before the limits
// fields existed and must never change.
func pinnedDefinition() *graph.Definition {
	return &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0, ID: "pin", Name: "ignored",
		Inputs: map[string]graph.InputDefinition{"x": {Type: graph.ValueTypeString, Required: true}},
		Nodes: map[string]graph.NodeDefinition{
			"a": {Type: "shell", Config: graph.NodeConfig{"command": "true"}, Inputs: map[string]graph.PortDefinition{"x": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"out": {Type: graph.ValueTypeString}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.x", To: "a.inputs.x"}},
		Outputs:            map[string]graph.OutputDefinition{"r": {From: "a.outputs.out"}},
	}
}

func TestSemanticVersionUnchangedWhenLimitsAbsent(t *testing.T) {
	got, err := yamlgraph.SemanticVersion(pinnedDefinition())
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256-v0:d292a865e1f48d172b5a551f3275c3fd2742e575d8fec065e02bf2c03fb63126"
	if got != want {
		t.Fatalf("version = %s, want %s", got, want)
	}
}
