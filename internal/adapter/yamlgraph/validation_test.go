package yamlgraph_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestValidateParallelExample(t *testing.T) {
	data, err := os.ReadFile("../../../.agents/graphs/hello-parallel.yaml")
	if err != nil {
		t.Fatalf("read example graph: %v", err)
	}

	result := graph.Validate(parseDefinition(t, string(data)))
	if !result.Valid() {
		t.Fatalf("Validate() errors = %#v, want no errors", result.Diagnostics)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Validate() diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestValidateReportsMissingSourceAndTargetNodes(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: missing-nodes
inputs: {}
nodes: {}
edges:
  - from: missing_source.outputs.value
    to: missing_target.inputs.value
outputs: {}
`))

	diagnostic := assertDiagnostic(t, result, "E_EDGE_SOURCE_NODE_NOT_FOUND", "edges[0].from")
	if !strings.Contains(diagnostic.Message, "missing_source.outputs.value") {
		t.Fatalf("source-node diagnostic = %q, want offending reference", diagnostic.Message)
	}
	assertDiagnostic(t, result, "E_EDGE_TARGET_NODE_NOT_FOUND", "edges[0].to")
	if result.Valid() {
		t.Fatal("Validate() Valid() = true, want false")
	}
}

func TestValidateReportsMissingInputAndOutput(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: missing-ports
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {present: {type: string}}
  consumer:
    type: shell
    inputs: {present: {type: string}}
edges:
  - from: producer.outputs.missing
    to: consumer.inputs.missing
outputs: {}
`))

	assertDiagnostic(t, result, "E_EDGE_SOURCE_OUTPUT_NOT_FOUND", "edges[0].from")
	assertDiagnostic(t, result, "E_EDGE_TARGET_INPUT_NOT_FOUND", "edges[0].to")
}

func TestValidateRejectsIncompatibleBindingTypes(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: incompatible-types
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {value: {type: string}}
  consumer:
    type: shell
    inputs: {value: {type: number, required: true}}
edges:
  - from: producer.outputs.value
    to: consumer.inputs.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_BINDING_TYPE_MISMATCH", "edges[0]")
}

func TestValidateDetectsTwoNodeCycle(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: two-node-cycle
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
`))

	diagnostic := assertDiagnostic(t, result, "E_GRAPH_CYCLE", "edges[1]")
	if !strings.Contains(diagnostic.Message, "first -> second -> first") {
		t.Fatalf("cycle diagnostic = %q, want cycle path", diagnostic.Message)
	}
}

func TestValidateDetectsLongerCycle(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: longer-cycle
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
  third:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
edges:
  - from: first.outputs.value
    to: second.inputs.value
  - from: second.outputs.value
    to: third.inputs.value
  - from: third.outputs.value
    to: first.inputs.value
outputs: {}
`))

	diagnostic := assertDiagnostic(t, result, "E_GRAPH_CYCLE", "edges[2]")
	if !strings.Contains(diagnostic.Message, "first -> second -> third -> first") {
		t.Fatalf("cycle diagnostic = %q, want cycle path", diagnostic.Message)
	}
}

func TestValidateDetectsSelfCycle(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: self-cycle
inputs: {}
nodes:
  worker:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
edges:
  - from: worker.outputs.value
    to: worker.inputs.value
outputs: {}
`))

	diagnostic := assertDiagnostic(t, result, "E_GRAPH_CYCLE", "edges[0]")
	if !strings.Contains(diagnostic.Message, "worker -> worker") || result.Valid() {
		t.Fatalf("self-cycle result = %#v, want blocking cycle with node path", result)
	}
}

func TestValidateReportsCyclesInDisconnectedComponents(t *testing.T) {
	definition := parseDefinition(t, `
schema_version: 0
id: disconnected-cycles
inputs: {}
nodes:
  alpha:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
  beta:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
  gamma:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
  delta:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
edges:
  - from: alpha.outputs.value
    to: beta.inputs.value
  - from: beta.outputs.value
    to: alpha.inputs.value
  - from: gamma.outputs.value
    to: delta.inputs.value
  - from: delta.outputs.value
    to: gamma.inputs.value
outputs: {}
`)

	first := graph.Validate(definition)
	var cycles []graph.Diagnostic
	for _, diagnostic := range first.Diagnostics {
		if diagnostic.Code == "E_GRAPH_CYCLE" {
			cycles = append(cycles, diagnostic)
		}
	}
	if len(cycles) != 2 {
		t.Fatalf("cycle diagnostics = %#v, want two independent cycles", cycles)
	}
	if cycles[0].Location.Path != "edges[1]" || !strings.Contains(cycles[0].Message, "alpha -> beta -> alpha") ||
		cycles[1].Location.Path != "edges[2]" || !strings.Contains(cycles[1].Message, "delta -> gamma -> delta") {
		t.Fatalf("cycle diagnostics = %#v, want stable paths for both components", cycles)
	}
	for range 20 {
		if next := graph.Validate(definition); !reflect.DeepEqual(first, next) {
			t.Fatalf("cycle diagnostics changed between runs: first = %#v, next = %#v", first, next)
		}
	}
}

func TestValidateReportsCycleDespiteIncompatibleEdge(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: incompatible-cycle
inputs: {}
nodes:
  first:
    type: shell
    inputs: {value: {type: string}}
    outputs: {value: {type: string}}
  second:
    type: shell
    inputs: {value: {type: number}}
    outputs: {value: {type: string}}
edges:
  - from: first.outputs.value
    to: second.inputs.value
  - from: second.outputs.value
    to: first.inputs.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_BINDING_TYPE_MISMATCH", "edges[0]")
	assertDiagnostic(t, result, "E_GRAPH_CYCLE", "edges[1]")
}

func TestValidateReportsMultipleIndependentErrors(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: multiple-errors
inputs: {}
nodes:
  consumer:
    type: shell
    inputs: {required: {type: string, required: true}}
edges:
  - from: missing.outputs.value
    to: consumer.inputs.missing
outputs:
  result: {from: missing.outputs.value}
`))

	for _, code := range []string{
		"E_EDGE_SOURCE_NODE_NOT_FOUND",
		"E_EDGE_TARGET_INPUT_NOT_FOUND",
		"E_REQUIRED_INPUT_UNBOUND",
		"E_GRAPH_OUTPUT_SOURCE_NODE_NOT_FOUND",
	} {
		assertDiagnostic(t, result, code, "")
	}
}

func TestValidateRequiredInputAndConflictingProviders(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: conflicting-providers
inputs:
  first: {type: string}
  second: {type: string}
nodes:
  consumer:
    type: shell
    inputs: {value: {type: string, required: true}}
edges: []
graph_input_bindings:
  - from: inputs.first
    to: consumer.inputs.value
  - from: inputs.second
    to: consumer.inputs.value
outputs: {}
`))

	diagnostic := assertDiagnostic(t, result, "E_INPUT_MULTIPLE_PROVIDERS", "nodes.consumer.inputs.value")
	if diagnostic.Location.GraphInputBindingIndex == nil || *diagnostic.Location.GraphInputBindingIndex != 1 {
		t.Fatalf("conflict location = %#v, want second binding index", diagnostic.Location)
	}
	if hasDiagnosticCode(result, "E_REQUIRED_INPUT_UNBOUND") {
		t.Fatalf("diagnostics = %#v, required input with providers must not be unbound", result.Diagnostics)
	}
}

func TestValidateGraphInputBindingSatisfiesRequiredInput(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: bound-required-input
inputs:
  value: {type: string, required: true}
nodes:
  worker:
    type: shell
    inputs: {value: {type: string, required: true}}
edges: []
graph_input_bindings:
  - from: inputs.value
    to: worker.inputs.value
outputs: {}
`))

	if !result.Valid() || len(result.Diagnostics) != 0 {
		t.Fatalf("valid graph input binding diagnostics = %#v, want none", result.Diagnostics)
	}
}

func TestValidateConflictingEdgesIdentifyLaterEdge(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: conflicting-edges
inputs: {}
nodes:
  first:
    type: shell
    outputs: {value: {type: string}}
  second:
    type: shell
    outputs: {value: {type: string}}
  consumer:
    type: shell
    inputs: {value: {type: string}}
edges:
  - from: first.outputs.value
    to: consumer.inputs.value
  - from: second.outputs.value
    to: consumer.inputs.value
outputs: {}
`))

	diagnostic := assertDiagnostic(t, result, "E_INPUT_MULTIPLE_PROVIDERS", "nodes.consumer.inputs.value")
	if diagnostic.Location.EdgeIndex == nil || *diagnostic.Location.EdgeIndex != 1 {
		t.Fatalf("conflict location = %#v, want second edge index", diagnostic.Location)
	}
}

func TestValidateInvalidPortTypeDoesNotSatisfyRequiredInput(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: invalid-port-type
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {value: {type: future_type}}
  consumer:
    type: shell
    inputs: {value: {type: future_type, required: true}}
edges:
  - from: producer.outputs.value
    to: consumer.inputs.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_VALUE_TYPE_INVALID", "nodes.producer.outputs.value.type")
	assertDiagnostic(t, result, "E_REQUIRED_INPUT_UNBOUND", "nodes.consumer.inputs.value")
}

func TestValidateGraphInputBindingAndOutputReferences(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: graph-references
inputs: {}
nodes:
  worker:
    type: shell
    inputs: {value: {type: string}}
    outputs: {present: {type: string}}
edges: []
graph_input_bindings:
  - from: inputs.missing
    to: worker.inputs.value
outputs:
  result: {from: worker.outputs.missing}
`))

	assertDiagnostic(t, result, "E_GRAPH_INPUT_NOT_FOUND", "graph_input_bindings[0].from")
	assertDiagnostic(t, result, "E_GRAPH_OUTPUT_SOURCE_OUTPUT_NOT_FOUND", "outputs.result.from")
}

func TestValidateRejectsIncompatibleGraphInputBindingTypes(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: incompatible-graph-input-binding
inputs:
  value: {type: number}
nodes:
  worker:
    type: shell
    inputs: {value: {type: string, required: true}}
edges: []
graph_input_bindings:
  - from: inputs.value
    to: worker.inputs.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_BINDING_TYPE_MISMATCH", "graph_input_bindings[0]")
	assertDiagnostic(t, result, "E_REQUIRED_INPUT_UNBOUND", "nodes.worker.inputs.value")
}

func TestValidateUnusedOutputIsWarningOnly(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: unused-output
inputs: {}
nodes:
  worker:
    type: shell
    outputs:
      result: {type: string}
      debug: {type: string}
edges: []
outputs:
  result: {from: worker.outputs.result}
`))

	assertDiagnostic(t, result, "W_DECLARED_OUTPUT_UNUSED", "nodes.worker.outputs.debug")
	if !result.Valid() {
		t.Fatalf("Validate() diagnostics = %#v, warning must not block validity", result.Diagnostics)
	}
}

func TestValidateInvalidEdgeLeavesOutputUnused(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: invalid-edge-unused-output
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {value: {type: string}}
edges:
  - from: producer.outputs.value
    to: missing.inputs.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_EDGE_TARGET_NODE_NOT_FOUND", "edges[0].to")
	warning := assertDiagnostic(t, result, "W_DECLARED_OUTPUT_UNUSED", "nodes.producer.outputs.value")
	if warning.Severity != graph.SeverityWarning || result.Valid() {
		t.Fatalf("mixed diagnostics = %#v, want blocking error plus warning", result.Diagnostics)
	}
}

func TestValidateIncompatibleEdgeLeavesOutputUnused(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: incompatible-edge-unused-output
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {value: {type: string}}
  consumer:
    type: shell
    inputs: {value: {type: number}}
edges:
  - from: producer.outputs.value
    to: consumer.inputs.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_BINDING_TYPE_MISMATCH", "edges[0]")
	assertDiagnostic(t, result, "W_DECLARED_OUTPUT_UNUSED", "nodes.producer.outputs.value")
}

func TestValidateDiagnosticsAreDeterministic(t *testing.T) {
	definition := parseDefinition(t, `
schema_version: 0
id: deterministic
inputs: {}
nodes:
  zeta:
    type: shell
    outputs: {value: {type: string}}
  alpha:
    type: shell
    outputs: {value: {type: string}}
edges:
  - from: missing.outputs.value
    to: zeta.inputs.value
outputs: {}
`)

	first := graph.Validate(definition)
	second := graph.Validate(definition)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Validate() first = %#v, second = %#v, want stable diagnostics", first, second)
	}
}

func TestValidateReportsRequiredFieldsAndInvalidIdentifiers(t *testing.T) {
	result := graph.Validate(&graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "Invalid ID",
	})

	diagnostic := assertDiagnostic(t, result, "E_GRAPH_ID_INVALID", "id")
	if diagnostic.Message == "" {
		t.Fatal("invalid graph-id diagnostic message is empty")
	}
	for _, path := range []string{"inputs", "nodes", "edges", "outputs"} {
		assertDiagnostic(t, result, "E_GRAPH_FIELD_REQUIRED", path)
	}

	emptyID := graph.Validate(&graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		Inputs:        map[string]graph.InputDefinition{},
		Nodes:         map[string]graph.NodeDefinition{},
		Edges:         []graph.EdgeDefinition{},
		Outputs:       map[string]graph.OutputDefinition{},
	})
	if diagnostic := assertDiagnostic(t, emptyID, "E_GRAPH_ID_REQUIRED", "id"); diagnostic.Message == "" {
		t.Fatal("required graph-id diagnostic message is empty")
	}
}

// An invalid node id is author text that may be a pasted secret, so the
// diagnostic points at the nodes collection and carries neither the id nor a
// path that contains it (F3.8 follow-up on author text in messages).
func TestValidateLocatesInvalidNodeIDAtNodesCollectionWithoutEchoingIt(t *testing.T) {
	result := graph.Validate(&graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "invalid-node-id",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"bad.id": {Type: "shell"},
		},
		Edges:   []graph.EdgeDefinition{},
		Outputs: map[string]graph.OutputDefinition{},
	})

	diagnostic := assertDiagnostic(t, result, "E_IDENTIFIER_INVALID", "nodes")
	if diagnostic.Location.NodeID != "" {
		t.Fatalf("diagnostic node id = %q, want empty", diagnostic.Location.NodeID)
	}
	if strings.Contains(diagnostic.Message, "bad.id") {
		t.Fatalf("message echoes the invalid id: %q", diagnostic.Message)
	}
}

func TestValidateRejectsMalformedReferences(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: malformed-references
inputs: {}
nodes: {}
edges:
  - from: producer.value
    to: consumer.value
outputs: {}
`))

	assertDiagnostic(t, result, "E_EDGE_SOURCE_REFERENCE_INVALID", "edges[0].from")
	assertDiagnostic(t, result, "E_EDGE_TARGET_REFERENCE_INVALID", "edges[0].to")
}

func parseDefinition(t *testing.T, data string) *graph.Definition {
	t.Helper()
	definition, err := yamlgraph.Parse([]byte(data))
	if err != nil {
		t.Fatalf("yamlgraph.Parse() error = %v", err)
	}
	return definition
}

func assertDiagnostic(t *testing.T, result graph.ValidationResult, code, path string) graph.Diagnostic {
	t.Helper()
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code && (path == "" || diagnostic.Location.Path == path) {
			return diagnostic
		}
	}
	t.Fatalf("diagnostics = %#v, want code %q at path %q", result.Diagnostics, code, path)
	return graph.Diagnostic{}
}

func hasDiagnosticCode(result graph.ValidationResult, code string) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestValidateRejectsUnknownNodeType(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: unknown-type
inputs: {}
nodes:
  worker:
    type: foo
edges: []
outputs: {}
`))
	diagnostic := assertDiagnostic(t, result, "E_NODE_TYPE_UNKNOWN", "nodes.worker.type")
	if result.Valid() || diagnostic.Severity != graph.SeverityError || diagnostic.Location.NodeID != "worker" {
		t.Fatalf("diagnostic = %#v valid=%v, want blocking error for node worker", diagnostic, result.Valid())
	}
	for _, want := range []string{`"foo"`, "worker"} {
		if !strings.Contains(diagnostic.Message, want) {
			t.Fatalf("message %q lacks %s", diagnostic.Message, want)
		}
	}
	for _, allowed := range []string{"shell", "agent", "gate", "human"} {
		if !strings.Contains(diagnostic.Suggestion, allowed) {
			t.Fatalf("suggestion %q lacks allowed type %s", diagnostic.Suggestion, allowed)
		}
	}
}

func TestValidateWarnsWhenEdgeTargetIgnoresInput(t *testing.T) {
	const base = `
schema_version: 0
id: edge-unused
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {text: {type: string}}
  consumer:
    type: shell
    inputs: {text: {type: string, required: true}}
    outputs: {done: {type: string}}
%s
edges:
  - from: producer.outputs.text
    to: consumer.inputs.text
outputs:
  done: {from: consumer.outputs.done}
`
	unmapped := graph.Validate(parseDefinition(t, strings.Replace(base, "%s", "", 1)))
	diagnostic := assertDiagnostic(t, unmapped, "W_EDGE_INPUT_UNUSED", "edges[0].to")
	if diagnostic.Severity != graph.SeverityWarning || diagnostic.Location.NodeID != "" && diagnostic.Location.NodeID != "consumer" || !unmapped.Valid() {
		t.Fatalf("diagnostic = %#v valid=%v, want nonblocking warning", diagnostic, unmapped.Valid())
	}
	if !strings.Contains(diagnostic.Suggestion, "input_env") {
		t.Fatalf("suggestion %q does not mention input_env", diagnostic.Suggestion)
	}

	mapped := graph.Validate(parseDefinition(t, strings.Replace(base, "%s", "    config:\n      input_env: {text: TEXT}", 1)))
	if hasDiagnosticCode(mapped, "W_EDGE_INPUT_UNUSED") {
		t.Fatalf("input_env mapping still warns: %#v", mapped.Diagnostics)
	}
}

func TestValidateEdgeInputConsumptionPerNodeType(t *testing.T) {
	result := graph.Validate(parseDefinition(t, `
schema_version: 0
id: consumers
inputs: {}
nodes:
  producer:
    type: shell
    outputs: {a: {type: string}, b: {type: string}, c: {type: string}, d: {type: string}}
  human:
    type: human
    inputs: {summary: {type: string}, other: {type: string}}
    outputs: {decision: {type: string, required: true}}
    config: {context_inputs: [summary]}
  agent:
    type: agent
    inputs: {task: {type: string, required: true}}
    outputs: {summary: {type: string}}
edges:
  - {from: producer.outputs.a, to: human.inputs.summary}
  - {from: producer.outputs.b, to: human.inputs.other}
  - {from: producer.outputs.c, to: agent.inputs.task}
outputs: {}
`))
	assertDiagnostic(t, result, "W_EDGE_INPUT_UNUSED", "edges[1].to")
	count := 0
	for _, d := range result.Diagnostics {
		if d.Code == "W_EDGE_INPUT_UNUSED" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("W_EDGE_INPUT_UNUSED count = %d, want 1 (diagnostics %#v)", count, result.Diagnostics)
	}
}

func TestValidateErrorsMatchesValidateErrors(t *testing.T) {
	docs := []string{
		"schema_version: 0\nid: a\ninputs: {}\nnodes:\n  p:\n    type: shell\n    outputs: {o: {type: string}}\n    config: {command: \"true\"}\n  c:\n    type: shell\n    inputs: {i: {type: number}}\n    config: {command: \"true\"}\nedges:\n  - from: p.outputs.o\n    to: c.inputs.i\n  - from: c.outputs.none\n    to: p.inputs.none\noutputs: {}\n",
		"schema_version: 0\nid: b\ninputs: {}\nnodes:\n  p:\n    type: shell\n    inputs: {i: {type: string}}\n    outputs: {o: {type: string}}\n    config: {command: \"true\"}\nedges:\n  - from: p.outputs.o\n    to: p.inputs.i\noutputs: {}\n",
	}
	for i, doc := range docs {
		def := parseDefinition(t, doc)
		var full, only []graph.Diagnostic
		for _, d := range graph.Validate(def).Diagnostics {
			if d.Severity == graph.SeverityError {
				full = append(full, d)
			}
		}
		for _, d := range graph.ValidateErrors(def).Diagnostics {
			if d.Severity != graph.SeverityError {
				t.Fatalf("doc %d: ValidateErrors returned %s %s", i, d.Severity, d.Code)
			}
			only = append(only, d)
		}
		if len(full) == 0 || !reflect.DeepEqual(full, only) {
			t.Fatalf("doc %d: errors differ\nValidate:       %+v\nValidateErrors: %+v", i, full, only)
		}
	}
}
