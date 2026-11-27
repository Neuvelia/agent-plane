package yamlgraph_test

import (
	"reflect"
	"testing"

	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
)

// roadmap: F2.1-AC2, F3.6-AC2
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

// TestJSONAndYAMLGraphsAreEquivalent pins D15: a JSON graph file is read by the
// YAML parser (YAML is a JSON superset) and yields the same definition and the
// same semantic version as the equivalent YAML graph.
func TestJSONAndYAMLGraphsAreEquivalent(t *testing.T) {
	const yamlGraph = `
schema_version: 0
id: json-yaml
name: Equivalent
inputs:
  topic: {type: string, required: true}
nodes:
  work:
    type: shell
    inputs: {topic: {type: string, required: true}}
    outputs: {stdout: {type: string}}
    config:
      command: echo
      args: [hello, "world"]
      input_env: {topic: TOPIC}
graph_input_bindings:
  - {from: inputs.topic, to: work.inputs.topic}
edges: []
outputs:
  result: {from: work.outputs.stdout}
`
	const jsonGraph = `{
  "schema_version": 0,
  "id": "json-yaml",
  "name": "Equivalent",
  "inputs": {"topic": {"type": "string", "required": true}},
  "nodes": {
    "work": {
      "type": "shell",
      "inputs": {"topic": {"type": "string", "required": true}},
      "outputs": {"stdout": {"type": "string"}},
      "config": {"command": "echo", "args": ["hello", "world"], "input_env": {"topic": "TOPIC"}}
    }
  },
  "graph_input_bindings": [{"from": "inputs.topic", "to": "work.inputs.topic"}],
  "edges": [],
  "outputs": {"result": {"from": "work.outputs.stdout"}}
}`
	fromYAML, err := yamlgraph.Parse([]byte(yamlGraph))
	if err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	fromJSON, err := yamlgraph.Parse([]byte(jsonGraph))
	if err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if !reflect.DeepEqual(fromYAML, fromJSON) {
		t.Fatalf("definitions differ:\nyaml=%#v\njson=%#v", fromYAML, fromJSON)
	}
	yamlVersion, err := yamlgraph.SemanticVersion(fromYAML)
	if err != nil {
		t.Fatal(err)
	}
	jsonVersion, err := yamlgraph.SemanticVersion(fromJSON)
	if err != nil {
		t.Fatal(err)
	}
	if yamlVersion == "" || yamlVersion != jsonVersion {
		t.Fatalf("graph_version differs: yaml=%q json=%q", yamlVersion, jsonVersion)
	}
}
