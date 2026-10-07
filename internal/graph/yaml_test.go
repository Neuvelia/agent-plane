package graph

import (
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseMinimalGraph(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: minimal
inputs: {}
nodes:
  only:
    type: shell
    outputs:
      stdout:
        type: string
edges: []
outputs:
  result:
    from: only.outputs.stdout
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if definition.SchemaVersion != SchemaVersionV0 {
		t.Fatalf("SchemaVersion = %d, want %d", definition.SchemaVersion, SchemaVersionV0)
	}
	if definition.ID != "minimal" {
		t.Fatalf("ID = %q, want %q", definition.ID, "minimal")
	}
	if len(definition.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1", len(definition.Nodes))
	}
	if got := definition.Nodes["only"].Outputs["stdout"].Type; got != ValueTypeString {
		t.Fatalf("only.outputs.stdout.type = %q, want %q", got, ValueTypeString)
	}
	if got := definition.Outputs["result"].From; got != NodeOutputReference("only.outputs.stdout") {
		t.Fatalf("outputs.result.from = %q, want %q", got, "only.outputs.stdout")
	}
}

func TestParseParallelExampleContracts(t *testing.T) {
	data, err := os.ReadFile("../../.agents/graphs/hello-parallel.yaml")
	if err != nil {
		t.Fatalf("read example graph: %v", err)
	}

	definition, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if definition.ID != "hello-parallel" {
		t.Fatalf("ID = %q, want %q", definition.ID, "hello-parallel")
	}
	if definition.Name != "Hello Parallel" {
		t.Fatalf("Name = %q, want %q", definition.Name, "Hello Parallel")
	}
	if len(definition.Nodes) != 3 {
		t.Fatalf("len(Nodes) = %d, want 3", len(definition.Nodes))
	}
	if got := definition.Nodes["left"].Type; got != "shell" {
		t.Fatalf("left.type = %q, want %q", got, "shell")
	}
	if got := definition.Nodes["left"].Outputs["stdout"].Type; got != ValueTypeString {
		t.Fatalf("left.outputs.stdout.type = %q, want %q", got, ValueTypeString)
	}
	if got := definition.Nodes["merge"].Inputs["left_text"].Type; got != ValueTypeString {
		t.Fatalf("merge.inputs.left_text.type = %q, want %q", got, ValueTypeString)
	}
	if got := definition.Nodes["merge"].Inputs["right_text"].Type; got != ValueTypeString {
		t.Fatalf("merge.inputs.right_text.type = %q, want %q", got, ValueTypeString)
	}

	wantEdges := []EdgeDefinition{
		{From: "left.outputs.stdout", To: "merge.inputs.left_text"},
		{From: "right.outputs.stdout", To: "merge.inputs.right_text"},
	}
	if !reflect.DeepEqual(definition.Edges, wantEdges) {
		t.Fatalf("Edges = %#v, want %#v", definition.Edges, wantEdges)
	}
	if got := definition.Outputs["result"].From; got != NodeOutputReference("merge.outputs.stdout") {
		t.Fatalf("outputs.result.from = %q, want %q", got, "merge.outputs.stdout")
	}
}

func TestParseGraphInputBindings(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: bound-input
inputs:
  message:
    type: string
    required: true
nodes:
  print:
    type: shell
    inputs:
      message:
        type: string
    outputs:
      stdout:
        type: string
edges: []
graph_input_bindings:
  - from: inputs.message
    to: print.inputs.message
outputs:
  result:
    from: print.outputs.stdout
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !definition.Inputs["message"].Required {
		t.Fatal("inputs.message.required = false, want true")
	}

	want := []GraphInputBindingDefinition{
		{From: "inputs.message", To: "print.inputs.message"},
	}
	if !reflect.DeepEqual(definition.GraphInputBindings, want) {
		t.Fatalf("GraphInputBindings = %#v, want %#v", definition.GraphInputBindings, want)
	}
}

func TestParseValueTypes(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: value-types
inputs:
  text: {type: string}
  count: {type: number}
  enabled: {type: boolean}
  payload: {type: json}
  artifact: {type: artifact_ref}
nodes: {}
edges: []
outputs: {}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := map[string]ValueType{
		"text":     ValueTypeString,
		"count":    ValueTypeNumber,
		"enabled":  ValueTypeBoolean,
		"payload":  ValueTypeJSON,
		"artifact": ValueTypeArtifactRef,
	}
	for name, wantType := range want {
		if got := definition.Inputs[name].Type; got != wantType {
			t.Errorf("inputs.%s.type = %q, want %q", name, got, wantType)
		}
	}
}

func TestParseUnsupportedSchemaVersion(t *testing.T) {
	for _, version := range []SchemaVersion{-1, 1} {
		t.Run(strconv.Itoa(int(version)), func(t *testing.T) {
			definition, err := Parse([]byte(`
schema_version: ` + strconv.Itoa(int(version)) + `
id: unsupported
inputs: {}
nodes: {}
edges: []
outputs: {}
`))
			if definition != nil {
				t.Fatalf("Parse() definition = %#v, want nil", definition)
			}
			if !errors.Is(err, ErrUnsupportedSchemaVersion) {
				t.Fatalf("Parse() error = %v, want ErrUnsupportedSchemaVersion", err)
			}

			var versionError *UnsupportedSchemaVersionError
			if !errors.As(err, &versionError) {
				t.Fatalf("Parse() error = %T, want *UnsupportedSchemaVersionError", err)
			}
			if versionError.Version != version {
				t.Fatalf("UnsupportedSchemaVersionError.Version = %d, want %d", versionError.Version, version)
			}
		})
	}
}

func TestParseMissingSchemaVersion(t *testing.T) {
	definition, err := Parse([]byte(`
id: missing-version
inputs: {}
nodes: {}
edges: []
outputs: {}
`))
	if definition != nil {
		t.Fatalf("Parse() definition = %#v, want nil", definition)
	}
	if !errors.Is(err, ErrMissingSchemaVersion) {
		t.Fatalf("Parse() error = %v, want ErrMissingSchemaVersion", err)
	}
}

func TestParseRejectsNonIntegerSchemaVersion(t *testing.T) {
	tests := map[string]string{
		"fractional": "0.5",
		"scientific": "0e0",
		"quoted":     `"0"`,
	}

	for name, version := range tests {
		t.Run(name, func(t *testing.T) {
			definition, err := Parse([]byte(`
schema_version: ` + version + `
id: invalid-version
inputs: {}
nodes: {}
edges: []
outputs: {}
`))
			if definition != nil {
				t.Fatalf("Parse() definition = %#v, want nil", definition)
			}
			if !errors.Is(err, ErrInvalidSchemaVersion) {
				t.Fatalf("Parse() error = %v, want ErrInvalidSchemaVersion", err)
			}
		})
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	tests := map[string]struct {
		yaml         string
		unknownField string
	}{
		"top level": {
			yaml: `
schema_version: 0
id: strict
secret: forbidden
inputs: {}
nodes: {}
edges: []
outputs: {}
`,
			unknownField: "secret",
		},
		"node": {
			yaml: `
schema_version: 0
id: strict
inputs: {}
nodes:
  one:
    type: shell
    typo: true
edges: []
outputs: {}
`,
			unknownField: "typo",
		},
		"graph input": {
			yaml: `
schema_version: 0
id: strict
inputs:
  value:
    type: string
    typo: true
nodes: {}
edges: []
outputs: {}
`,
			unknownField: "typo",
		},
		"node port": {
			yaml: `
schema_version: 0
id: strict
inputs: {}
nodes:
  one:
    type: shell
    inputs:
      value:
        type: string
        typo: true
edges: []
outputs: {}
`,
			unknownField: "typo",
		},
		"edge": {
			yaml: `
schema_version: 0
id: strict
inputs: {}
nodes: {}
edges:
  - from: one.outputs.value
    to: two.inputs.value
    typo: true
outputs: {}
`,
			unknownField: "typo",
		},
		"graph input binding": {
			yaml: `
schema_version: 0
id: strict
inputs: {}
nodes: {}
edges: []
graph_input_bindings:
  - from: inputs.value
    to: one.inputs.value
    typo: true
outputs: {}
`,
			unknownField: "typo",
		},
		"output": {
			yaml: `
schema_version: 0
id: strict
inputs: {}
nodes: {}
edges: []
outputs:
  value:
    from: one.outputs.value
    typo: true
`,
			unknownField: "typo",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			definition, err := Parse([]byte(test.yaml))
			if definition != nil {
				t.Fatalf("Parse() definition = %#v, want nil", definition)
			}
			if err == nil {
				t.Fatal("Parse() error = nil, want unknown-field error")
			}
			if !strings.Contains(err.Error(), test.unknownField) {
				t.Fatalf("Parse() error = %q, want field name %q", err, test.unknownField)
			}
		})
	}
}

func TestParseRejectsDuplicateNodeIDs(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: duplicate-nodes
inputs: {}
nodes:
  worker: {type: shell}
  worker: {type: shell}
edges: []
outputs: {}
`))
	if definition != nil || err == nil {
		t.Fatalf("Parse() = (%#v, %v), want duplicate node ID error and no definition", definition, err)
	}
}

func TestParseMalformedYAMLReturnsNoPartialDefinition(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: malformed
inputs: {}
nodes: [
`))
	if definition != nil {
		t.Fatalf("Parse() definition = %#v, want nil", definition)
	}
	if err == nil {
		t.Fatal("Parse() error = nil, want malformed-YAML error")
	}
	if !strings.Contains(err.Error(), "parse graph definition") {
		t.Fatalf("Parse() error = %q, want graph parsing context", err)
	}
}

func TestParseRejectsMultipleDocuments(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: first
inputs: {}
nodes: {}
edges: []
outputs: {}
---
schema_version: 0
id: second
inputs: {}
nodes: {}
edges: []
outputs: {}
`))
	if definition != nil {
		t.Fatalf("Parse() definition = %#v, want nil", definition)
	}
	if !errors.Is(err, ErrMultipleDocuments) {
		t.Fatalf("Parse() error = %v, want ErrMultipleDocuments", err)
	}
}

func TestParseAllowsMetadataAndConfigFields(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: extensions
metadata:
  description: Non-semantic information
  layout:
    x: 12
    collapsed: true
inputs: {}
nodes:
  one:
    type: future-node
    config:
      command: printf
      args: ["%s", "value"]
      options:
        nested: true
    outputs: {}
edges: []
outputs: {}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	wantMetadata := Metadata{
		"description": "Non-semantic information",
		"layout": Metadata{
			"x":         12,
			"collapsed": true,
		},
	}
	if !reflect.DeepEqual(definition.Metadata, wantMetadata) {
		t.Fatalf("Metadata = %#v, want %#v", definition.Metadata, wantMetadata)
	}

	wantConfig := NodeConfig{
		"command": "printf",
		"args":    []any{"%s", "value"},
		"options": NodeConfig{"nested": true},
	}
	if !reflect.DeepEqual(definition.Nodes["one"].Config, wantConfig) {
		t.Fatalf("Config = %#v, want %#v", definition.Nodes["one"].Config, wantConfig)
	}
}

func TestParseTreatsConfigStringsAsInertData(t *testing.T) {
	definition, err := Parse([]byte(`
schema_version: 0
id: inert-config
inputs: {}
nodes:
  one:
    type: shell
    config:
      template: "{{ inputs.message }}"
      environment: "${TOKEN}"
      command: "printf secret"
      url: "https://example.invalid/resource"
    outputs: {}
edges: []
outputs: {}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	want := NodeConfig{
		"template":    "{{ inputs.message }}",
		"environment": "${TOKEN}",
		"command":     "printf secret",
		"url":         "https://example.invalid/resource",
	}
	if got := definition.Nodes["one"].Config; !reflect.DeepEqual(got, want) {
		t.Fatalf("Config = %#v, want %#v", got, want)
	}
}

func TestParseDoesNotPerformSemanticValidation(t *testing.T) {
	tests := map[string]string{
		"unknown references": `
schema_version: 0
id: unknown-reference
inputs: {}
nodes: {}
edges:
  - from: missing.outputs.value
    to: also_missing.inputs.value
outputs: {}
`,
		"cycle": `
schema_version: 0
id: cycle
inputs: {}
nodes:
  first:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
  second:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
edges:
  - from: first.outputs.value
    to: second.inputs.value
  - from: second.outputs.value
    to: first.inputs.value
outputs: {}
`,
		"unknown value type": `
schema_version: 0
id: unknown-type
inputs:
  value: {type: future_type}
nodes: {}
edges: []
outputs: {}
`,
	}

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			definition, err := Parse([]byte(data))
			if err != nil {
				t.Fatalf("Parse() error = %v, want structural parse success", err)
			}
			if definition == nil {
				t.Fatal("Parse() definition = nil, want parsed definition")
			}
		})
	}
}

func TestRoundTripPreservesDefinition(t *testing.T) {
	data, err := os.ReadFile("../../.agents/graphs/hello-parallel.yaml")
	if err != nil {
		t.Fatalf("read example graph: %v", err)
	}
	original, err := Parse(data)
	if err != nil {
		t.Fatalf("initial Parse() error = %v", err)
	}

	encoded, err := Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTripped, err := Parse(encoded)
	if err != nil {
		t.Fatalf("round-trip Parse() error = %v", err)
	}
	if !reflect.DeepEqual(roundTripped, original) {
		t.Fatalf("round-tripped definition = %#v, want %#v", roundTripped, original)
	}
}

func TestMarshalRejectsUnsupportedSchemaVersion(t *testing.T) {
	definition := &Definition{SchemaVersion: 1}
	data, err := Marshal(definition)
	if data != nil {
		t.Fatalf("Marshal() data = %q, want nil", data)
	}
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Marshal() error = %v, want ErrUnsupportedSchemaVersion", err)
	}
}

func TestMarshalRejectsNilDefinition(t *testing.T) {
	data, err := Marshal(nil)
	if data != nil {
		t.Fatalf("Marshal() data = %q, want nil", data)
	}
	if err == nil {
		t.Fatal("Marshal() error = nil, want nil-definition error")
	}
}
