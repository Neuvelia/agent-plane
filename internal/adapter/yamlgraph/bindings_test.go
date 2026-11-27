package yamlgraph_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/graph"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestGraphInputResolvesToIndependentConsumers(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "independent-consumers",
		Inputs: map[string]graph.InputDefinition{
			"message": {Type: graph.ValueTypeString, Required: true},
		},
		Nodes: map[string]graph.NodeDefinition{
			"left":  {Type: "shell", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}},
			"right": {Type: "shell", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}},
		},
		Edges: []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{
			{From: "inputs.message", To: "left.inputs.text"},
			{From: "inputs.message", To: "right.inputs.text"},
		},
		Outputs: map[string]graph.OutputDefinition{},
	}

	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() || plan == nil {
		t.Fatalf("CompileBindings() = (%#v, %#v), want valid plan", plan, diagnostics)
	}
	for _, nodeID := range []string{"left", "right"} {
		dependencies, err := plan.Dependencies(nodeID)
		if err != nil || len(dependencies) != 0 {
			t.Fatalf("Dependencies(%q) = (%v, %v), want no dependencies", nodeID, dependencies, err)
		}
		inputs, result := plan.ResolveNodeInputs(nodeID, map[string]graph.Value{"message": graph.NewStringValue("hello")}, nil)
		if !result.Valid() {
			t.Fatalf("ResolveNodeInputs(%q) diagnostics = %#v", nodeID, result.Diagnostics)
		}
		if got, ok := inputs["text"].AsString(); !ok || got != "hello" {
			t.Fatalf("ResolveNodeInputs(%q) text = (%q, %v), want hello", nodeID, got, ok)
		}
	}

	if _, err := plan.Dependencies("missing"); err == nil {
		t.Fatal("Dependencies(missing) error = nil, want unknown-node error")
	}
}

func TestMissingRequiredGraphInputBlocksResolution(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "missing-value",
		Inputs:        map[string]graph.InputDefinition{"message": {Type: graph.ValueTypeString, Required: true}},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.message", To: "worker.inputs.text"}},
		Outputs:            map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}

	inputs, result := plan.ResolveNodeInputs("worker", nil, nil)
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want blocking error and no inputs", inputs, result)
	}
	diagnostic := assertDiagnostic(t, result, "E_GRAPH_INPUT_VALUE_MISSING", "nodes.worker.inputs.text")
	if diagnostic.Location.NodeID != "worker" || diagnostic.Location.GraphInputBindingIndex == nil || *diagnostic.Location.GraphInputBindingIndex != 0 {
		t.Fatalf("missing value location = %#v, want worker and binding 0", diagnostic.Location)
	}
	assertDiagnostic(t, plan.ValidateGraphInputs(nil), "E_GRAPH_INPUT_VALUE_MISSING", "inputs.message")
}

func TestSuppliedValueTypeMismatchBlocksResolution(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "runtime-type",
		Inputs:        map[string]graph.InputDefinition{"count": {Type: graph.ValueTypeNumber}},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"count": {Type: graph.ValueTypeNumber}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.count", To: "worker.inputs.count"}},
		Outputs:            map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	values := map[string]graph.Value{"count": graph.NewStringValue("12")}
	assertDiagnostic(t, plan.ValidateGraphInputs(values), "E_GRAPH_INPUT_VALUE_TYPE_MISMATCH", "inputs.count")
	inputs, result := plan.ResolveNodeInputs("worker", values, nil)
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want blocking type error", inputs, result)
	}
	assertDiagnostic(t, result, "E_BOUND_VALUE_TYPE_MISMATCH", "nodes.worker.inputs.count")
}

func TestNodeOutputsResolveToDistinctInputs(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "node-bindings",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]graph.PortDefinition{
				"text":  {Type: graph.ValueTypeString},
				"count": {Type: graph.ValueTypeNumber},
			}},
			"consumer": {Type: "shell", Inputs: map[string]graph.PortDefinition{
				"text":  {Type: graph.ValueTypeString, Required: true},
				"count": {Type: graph.ValueTypeNumber, Required: true},
			}},
		},
		Edges: []graph.EdgeDefinition{
			{From: "producer.outputs.text", To: "consumer.inputs.text"},
			{From: "producer.outputs.count", To: "consumer.inputs.count"},
		},
		Outputs: map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	dependencies, err := plan.Dependencies("consumer")
	if err != nil || !reflect.DeepEqual(dependencies, []string{"producer"}) {
		t.Fatalf("Dependencies(consumer) = (%v, %v), want producer once", dependencies, err)
	}
	number, err := graph.NewNumberValue(json.Number("9007199254740993"))
	if err != nil {
		t.Fatalf("NewNumberValue() error = %v", err)
	}
	inputs, result := plan.ResolveNodeInputs("consumer", nil, map[string]map[string]graph.Value{
		"producer": {"text": graph.NewStringValue("done"), "count": number},
	})
	if !result.Valid() {
		t.Fatalf("ResolveNodeInputs() diagnostics = %#v", result.Diagnostics)
	}
	if got, ok := inputs["text"].AsString(); !ok || got != "done" {
		t.Fatalf("text = (%q, %v), want done", got, ok)
	}
	if got, ok := inputs["count"].AsNumber(); !ok || got != json.Number("9007199254740993") {
		t.Fatalf("count = (%q, %v), want exact number", got, ok)
	}
}

func TestMissingBoundNodeOutputBlocksResolution(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "missing-output",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
			"consumer": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		},
		Edges:   []graph.EdgeDefinition{{From: "producer.outputs.value", To: "consumer.inputs.value"}},
		Outputs: map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	inputs, result := plan.ResolveNodeInputs("consumer", nil, map[string]map[string]graph.Value{"producer": {}})
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want blocking error", inputs, result)
	}
	diagnostic := assertDiagnostic(t, result, "E_BOUND_OUTPUT_VALUE_MISSING", "nodes.consumer.inputs.value")
	if diagnostic.Location.EdgeIndex == nil || *diagnostic.Location.EdgeIndex != 0 {
		t.Fatalf("missing output location = %#v, want edge 0", diagnostic.Location)
	}
}

func TestCompileBindingsRejectsInvalidReferencesAndProviders(t *testing.T) {
	cases := []struct {
		name  string
		edges []graph.EdgeDefinition
		code  string
	}{
		{"source node", []graph.EdgeDefinition{{From: "missing.outputs.value", To: "consumer.inputs.value"}}, "E_EDGE_SOURCE_NODE_NOT_FOUND"},
		{"target node", []graph.EdgeDefinition{{From: "producer.outputs.value", To: "missing.inputs.value"}}, "E_EDGE_TARGET_NODE_NOT_FOUND"},
		{"source output", []graph.EdgeDefinition{{From: "producer.outputs.missing", To: "consumer.inputs.value"}}, "E_EDGE_SOURCE_OUTPUT_NOT_FOUND"},
		{"target input", []graph.EdgeDefinition{{From: "producer.outputs.value", To: "consumer.inputs.missing"}}, "E_EDGE_TARGET_INPUT_NOT_FOUND"},
		{"duplicate provider", []graph.EdgeDefinition{
			{From: "producer.outputs.value", To: "consumer.inputs.value"},
			{From: "producer.outputs.value", To: "consumer.inputs.value"},
		}, "E_INPUT_MULTIPLE_PROVIDERS"},
		{"wrong direction", []graph.EdgeDefinition{{From: "producer.inputs.value", To: "consumer.inputs.value"}}, "E_EDGE_SOURCE_REFERENCE_INVALID"},
		{"no binding", []graph.EdgeDefinition{}, "E_REQUIRED_INPUT_UNBOUND"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition := &graph.Definition{
				SchemaVersion: graph.SchemaVersionV0,
				ID:            "invalid-binding",
				Inputs:        map[string]graph.InputDefinition{},
				Nodes: map[string]graph.NodeDefinition{
					"producer": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
					"consumer": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString, Required: true}}},
				},
				Edges:   test.edges,
				Outputs: map[string]graph.OutputDefinition{},
			}
			plan, result := graph.CompileBindings(definition)
			if plan != nil || result.Valid() {
				t.Fatalf("CompileBindings() = (%#v, %#v), want no plan", plan, result)
			}
			assertDiagnostic(t, result, test.code, "")
		})
	}
}

func TestStructuredJSONBindingPreservesValueWithoutSharingBytes(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "json-binding",
		Inputs:        map[string]graph.InputDefinition{"payload": {Type: graph.ValueTypeJSON}},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"payload": {Type: graph.ValueTypeJSON}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.payload", To: "worker.inputs.payload"}},
		Outputs:            map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	raw := json.RawMessage(`{"items":[1,true,{"name":"ready"}]}`)
	value, err := graph.NewJSONValue(raw)
	if err != nil {
		t.Fatalf("NewJSONValue() error = %v", err)
	}
	raw[0] = '['
	inputs, result := plan.ResolveNodeInputs("worker", map[string]graph.Value{"payload": value}, nil)
	if !result.Valid() {
		t.Fatalf("ResolveNodeInputs() diagnostics = %#v", result.Diagnostics)
	}
	got, ok := inputs["payload"].AsJSON()
	if !ok || string(got) != `{"items":[1,true,{"name":"ready"}]}` {
		t.Fatalf("resolved JSON = (%s, %v)", got, ok)
	}
	got[0] = '['
	again, _ := inputs["payload"].AsJSON()
	if string(again) != `{"items":[1,true,{"name":"ready"}]}` {
		t.Fatalf("JSON accessor leaked mutable bytes: %s", again)
	}
}

func TestArtifactReferencePassesThroughWithoutLoading(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "artifact-binding",
		Inputs:        map[string]graph.InputDefinition{"report": {Type: graph.ValueTypeArtifactRef}},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"report": {Type: graph.ValueTypeArtifactRef, Required: true}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.report", To: "worker.inputs.report"}},
		Outputs:            map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	value, err := graph.NewArtifactRefValue("artifact:does-not-exist")
	if err != nil {
		t.Fatalf("NewArtifactRefValue() error = %v", err)
	}
	inputs, result := plan.ResolveNodeInputs("worker", map[string]graph.Value{"report": value}, nil)
	if !result.Valid() {
		t.Fatalf("ResolveNodeInputs() diagnostics = %#v", result.Diagnostics)
	}
	if got, ok := inputs["report"].AsArtifactRef(); !ok || got != "artifact:does-not-exist" {
		t.Fatalf("artifact reference = (%q, %v), want opaque handle", got, ok)
	}
}

func TestOptionalGraphInputCanRemainUnset(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "optional-input",
		Inputs:        map[string]graph.InputDefinition{"note": {Type: graph.ValueTypeString}},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"note": {Type: graph.ValueTypeString}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.note", To: "worker.inputs.note"}},
		Outputs:            map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	inputs, result := plan.ResolveNodeInputs("worker", nil, nil)
	if !result.Valid() || len(inputs) != 0 {
		t.Fatalf("optional ResolveNodeInputs() = (%#v, %#v), want empty valid inputs", inputs, result)
	}
}

func TestMissingRequiredGraphInputBlocksOptionalTargetResolution(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "required-source-optional-target",
		Inputs:        map[string]graph.InputDefinition{"note": {Type: graph.ValueTypeString, Required: true}},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{"note": {Type: graph.ValueTypeString}}},
		},
		Edges:              []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.note", To: "worker.inputs.note"}},
		Outputs:            map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	inputs, result := plan.ResolveNodeInputs("worker", nil, nil)
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want required-source error", inputs, result)
	}
	assertDiagnostic(t, result, "E_GRAPH_INPUT_VALUE_MISSING", "nodes.worker.inputs.note")
}

func TestCompiledPlanAllowsWarningsButRejectsDeclaredTypeMismatch(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "warning-only",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]graph.PortDefinition{"unused": {Type: graph.ValueTypeString}}},
		},
		Edges:   []graph.EdgeDefinition{},
		Outputs: map[string]graph.OutputDefinition{},
	}
	plan, result := graph.CompileBindings(definition)
	if plan == nil || !result.Valid() {
		t.Fatalf("warning-only CompileBindings() = (%#v, %#v), want plan", plan, result)
	}
	assertDiagnostic(t, result, "W_DECLARED_OUTPUT_UNUSED", "nodes.producer.outputs.unused")

	definition.Nodes["consumer"] = graph.NodeDefinition{Type: "shell", Inputs: map[string]graph.PortDefinition{"count": {Type: graph.ValueTypeNumber}}}
	definition.Edges = []graph.EdgeDefinition{{From: "producer.outputs.unused", To: "consumer.inputs.count"}}
	plan, result = graph.CompileBindings(definition)
	if plan != nil || result.Valid() {
		t.Fatalf("mismatched CompileBindings() = (%#v, %#v), want blocking error", plan, result)
	}
	assertDiagnostic(t, result, "E_BINDING_TYPE_MISMATCH", "edges[0]")
}

func TestBindingResolutionAndDiagnosticsAreDeterministic(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "deterministic-bindings",
		Inputs: map[string]graph.InputDefinition{
			"first":  {Type: graph.ValueTypeString},
			"second": {Type: graph.ValueTypeString},
		},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]graph.PortDefinition{
				"first":  {Type: graph.ValueTypeString, Required: true},
				"second": {Type: graph.ValueTypeString, Required: true},
			}},
		},
		Edges: []graph.EdgeDefinition{},
		GraphInputBindings: []graph.GraphInputBindingDefinition{
			{From: "inputs.second", To: "worker.inputs.second"},
			{From: "inputs.first", To: "worker.inputs.first"},
		},
		Outputs: map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	firstValues := map[string]graph.Value{"first": graph.NewStringValue("A"), "second": graph.NewStringValue("B")}
	secondValues := make(map[string]graph.Value)
	secondValues["second"] = graph.NewStringValue("B")
	secondValues["first"] = graph.NewStringValue("A")
	firstInputs, firstResult := plan.ResolveNodeInputs("worker", firstValues, nil)
	secondInputs, secondResult := plan.ResolveNodeInputs("worker", secondValues, nil)
	if !reflect.DeepEqual(firstInputs, secondInputs) || !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("resolution changed with input map order: (%#v, %#v) vs (%#v, %#v)", firstInputs, firstResult, secondInputs, secondResult)
	}
	_, firstMissing := plan.ResolveNodeInputs("worker", nil, nil)
	_, secondMissing := plan.ResolveNodeInputs("worker", nil, nil)
	if !reflect.DeepEqual(firstMissing, secondMissing) || len(firstMissing.Diagnostics) != 2 {
		t.Fatalf("missing-value diagnostics differ: %#v vs %#v", firstMissing, secondMissing)
	}
	if firstMissing.Diagnostics[0].Location.Path != "nodes.worker.inputs.first" || firstMissing.Diagnostics[1].Location.Path != "nodes.worker.inputs.second" {
		t.Fatalf("diagnostic order = %#v, want input-name order", firstMissing.Diagnostics)
	}
}

func TestNilBindingPlanReturnsStructuredErrors(t *testing.T) {
	var plan *graph.BindingPlan
	if _, err := plan.Dependencies("worker"); err == nil {
		t.Fatal("nil Dependencies() error = nil")
	}
	assertDiagnostic(t, plan.ValidateGraphInputs(nil), "E_BINDING_PLAN_NIL", "")
	inputs, result := plan.ResolveNodeInputs("worker", nil, nil)
	if inputs != nil || result.Valid() {
		t.Fatalf("nil ResolveNodeInputs() = (%#v, %#v), want blocking error", inputs, result)
	}
	assertDiagnostic(t, result, "E_BINDING_PLAN_NIL", "")
}

// roadmap: F0.3-AC1
func TestUnboundNodesHaveNoDependencies(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "unbound-nodes",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"left":  {Type: "shell"},
			"right": {Type: "shell"},
		},
		Edges:   []graph.EdgeDefinition{},
		Outputs: map[string]graph.OutputDefinition{},
	}
	plan, result := graph.CompileBindings(definition)
	if plan == nil || !result.Valid() {
		t.Fatalf("CompileBindings() = (%#v, %#v), want valid plan", plan, result)
	}
	for _, nodeID := range []string{"left", "right"} {
		dependencies, err := plan.Dependencies(nodeID)
		if err != nil || len(dependencies) != 0 {
			t.Fatalf("Dependencies(%s) = (%v, %v), want empty", nodeID, dependencies, err)
		}
	}
}

func TestCompiledDependenciesAreSortedAndIndependentOfDefinitionMutation(t *testing.T) {
	data, err := os.ReadFile("../../../.agents/graphs/hello-parallel.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	definition, err := yamlgraph.Parse(data)
	if err != nil {
		t.Fatalf("yamlgraph.Parse() error = %v", err)
	}
	plan, result := graph.CompileBindings(definition)
	if !result.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", result.Diagnostics)
	}
	definition.Edges = nil
	definition.Nodes["merge"] = graph.NodeDefinition{Type: "shell"}
	dependencies, err := plan.Dependencies("merge")
	if err != nil || !reflect.DeepEqual(dependencies, []string{"left", "right"}) {
		t.Fatalf("Dependencies(merge) = (%v, %v), want left/right", dependencies, err)
	}
	dependencies[0] = "mutated"
	again, _ := plan.Dependencies("merge")
	if !reflect.DeepEqual(again, []string{"left", "right"}) {
		t.Fatalf("Dependencies() leaked mutable slice: %v", again)
	}
	inputs, resolution := plan.ResolveNodeInputs("merge", nil, map[string]map[string]graph.Value{
		"left":  {"stdout": graph.NewStringValue("L")},
		"right": {"stdout": graph.NewStringValue("R")},
	})
	if !resolution.Valid() {
		t.Fatalf("ResolveNodeInputs() diagnostics = %#v", resolution.Diagnostics)
	}
	if got, ok := inputs["left_text"].AsString(); !ok || got != "L" {
		t.Fatalf("left_text = (%q, %v), want L", got, ok)
	}
	if got, ok := inputs["right_text"].AsString(); !ok || got != "R" {
		t.Fatalf("right_text = (%q, %v), want R", got, ok)
	}
}

func TestUnknownGraphInputAndWrongNodeOutputTypeHaveStructuredErrors(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "runtime-errors",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
			"consumer": {Type: "shell", Inputs: map[string]graph.PortDefinition{"value": {Type: graph.ValueTypeString}}},
		},
		Edges:   []graph.EdgeDefinition{{From: "producer.outputs.value", To: "consumer.inputs.value"}},
		Outputs: map[string]graph.OutputDefinition{},
	}
	plan, diagnostics := graph.CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	assertDiagnostic(t, plan.ValidateGraphInputs(map[string]graph.Value{"typo": graph.NewStringValue("unexpected")}), "E_GRAPH_INPUT_VALUE_UNKNOWN", "inputs")
	inputs, result := plan.ResolveNodeInputs("consumer", nil, map[string]map[string]graph.Value{
		"producer": {"value": graph.NewBooleanValue(true)},
	})
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want blocking type error", inputs, result)
	}
	diagnostic := assertDiagnostic(t, result, "E_BOUND_VALUE_TYPE_MISMATCH", "nodes.consumer.inputs.value")
	if diagnostic.Location.EdgeIndex == nil || *diagnostic.Location.EdgeIndex != 0 {
		t.Fatalf("type error location = %#v, want edge 0", diagnostic.Location)
	}
}

func TestResolveGraphOutputsUsesDeclaredReferencesAndSortedDiagnostics(t *testing.T) {
	definition := &graph.Definition{
		SchemaVersion: graph.SchemaVersionV0,
		ID:            "published-outputs",
		Inputs:        map[string]graph.InputDefinition{},
		Nodes: map[string]graph.NodeDefinition{
			"worker": {Type: "shell", Outputs: map[string]graph.PortDefinition{
				"first": {Type: graph.ValueTypeString},
				"last":  {Type: graph.ValueTypeBoolean},
			}},
		},
		Edges: []graph.EdgeDefinition{},
		Outputs: map[string]graph.OutputDefinition{
			"zeta":  {From: "worker.outputs.last"},
			"alpha": {From: "worker.outputs.first"},
		},
	}
	plan, validation := graph.CompileBindings(definition)
	if !validation.Valid() {
		t.Fatalf("CompileBindings() = %#v", validation.Diagnostics)
	}
	definition.Outputs["alpha"] = graph.OutputDefinition{From: "worker.outputs.last"}
	outputs, result := plan.ResolveGraphOutputs(map[string]map[string]graph.Value{
		"worker": {"first": graph.NewStringValue("ready"), "last": graph.NewBooleanValue(false)},
	})
	if !result.Valid() || len(outputs) != 2 {
		t.Fatalf("ResolveGraphOutputs() = (%#v, %#v)", outputs, result)
	}
	if got, ok := outputs["alpha"].AsString(); !ok || got != "ready" {
		t.Fatalf("alpha = (%q, %v), want ready", got, ok)
	}
	if got, ok := outputs["zeta"].AsBoolean(); !ok || got {
		t.Fatalf("zeta = (%v, %v), want false", got, ok)
	}

	outputs, result = plan.ResolveGraphOutputs(map[string]map[string]graph.Value{
		"worker": {"first": graph.NewBooleanValue(true)},
	})
	if outputs != nil || len(result.Diagnostics) != 2 {
		t.Fatalf("invalid outputs = (%#v, %#v)", outputs, result)
	}
	if result.Diagnostics[0].Code != "E_GRAPH_OUTPUT_VALUE_TYPE_MISMATCH" || result.Diagnostics[0].Location.Path != "outputs.alpha.from" ||
		result.Diagnostics[1].Code != "E_GRAPH_OUTPUT_VALUE_MISSING" || result.Diagnostics[1].Location.Path != "outputs.zeta.from" {
		t.Fatalf("diagnostic order/content = %#v", result.Diagnostics)
	}
}
