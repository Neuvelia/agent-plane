package graph

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestGraphInputResolvesToIndependentConsumers(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "independent-consumers",
		Inputs: map[string]InputDefinition{
			"message": {Type: ValueTypeString, Required: true},
		},
		Nodes: map[string]NodeDefinition{
			"left":  {Type: "shell", Inputs: map[string]PortDefinition{"text": {Type: ValueTypeString, Required: true}}},
			"right": {Type: "shell", Inputs: map[string]PortDefinition{"text": {Type: ValueTypeString, Required: true}}},
		},
		Edges: []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{
			{From: "inputs.message", To: "left.inputs.text"},
			{From: "inputs.message", To: "right.inputs.text"},
		},
		Outputs: map[string]OutputDefinition{},
	}

	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() || plan == nil {
		t.Fatalf("CompileBindings() = (%#v, %#v), want valid plan", plan, diagnostics)
	}
	for _, nodeID := range []string{"left", "right"} {
		dependencies, err := plan.Dependencies(nodeID)
		if err != nil || len(dependencies) != 0 {
			t.Fatalf("Dependencies(%q) = (%v, %v), want no dependencies", nodeID, dependencies, err)
		}
		inputs, result := plan.ResolveNodeInputs(nodeID, map[string]Value{"message": NewStringValue("hello")}, nil)
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
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "missing-value",
		Inputs:        map[string]InputDefinition{"message": {Type: ValueTypeString, Required: true}},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{"text": {Type: ValueTypeString, Required: true}}},
		},
		Edges:              []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.message", To: "worker.inputs.text"}},
		Outputs:            map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
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
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "runtime-type",
		Inputs:        map[string]InputDefinition{"count": {Type: ValueTypeNumber}},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{"count": {Type: ValueTypeNumber}}},
		},
		Edges:              []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.count", To: "worker.inputs.count"}},
		Outputs:            map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	values := map[string]Value{"count": NewStringValue("12")}
	assertDiagnostic(t, plan.ValidateGraphInputs(values), "E_GRAPH_INPUT_VALUE_TYPE_MISMATCH", "inputs.count")
	inputs, result := plan.ResolveNodeInputs("worker", values, nil)
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want blocking type error", inputs, result)
	}
	assertDiagnostic(t, result, "E_BOUND_VALUE_TYPE_MISMATCH", "nodes.worker.inputs.count")
}

func TestNodeOutputsResolveToDistinctInputs(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "node-bindings",
		Inputs:        map[string]InputDefinition{},
		Nodes: map[string]NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]PortDefinition{
				"text":  {Type: ValueTypeString},
				"count": {Type: ValueTypeNumber},
			}},
			"consumer": {Type: "shell", Inputs: map[string]PortDefinition{
				"text":  {Type: ValueTypeString, Required: true},
				"count": {Type: ValueTypeNumber, Required: true},
			}},
		},
		Edges: []EdgeDefinition{
			{From: "producer.outputs.text", To: "consumer.inputs.text"},
			{From: "producer.outputs.count", To: "consumer.inputs.count"},
		},
		Outputs: map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	dependencies, err := plan.Dependencies("consumer")
	if err != nil || !reflect.DeepEqual(dependencies, []string{"producer"}) {
		t.Fatalf("Dependencies(consumer) = (%v, %v), want producer once", dependencies, err)
	}
	number, err := NewNumberValue(json.Number("9007199254740993"))
	if err != nil {
		t.Fatalf("NewNumberValue() error = %v", err)
	}
	inputs, result := plan.ResolveNodeInputs("consumer", nil, map[string]map[string]Value{
		"producer": {"text": NewStringValue("done"), "count": number},
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
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "missing-output",
		Inputs:        map[string]InputDefinition{},
		Nodes: map[string]NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]PortDefinition{"value": {Type: ValueTypeString}}},
			"consumer": {Type: "shell", Inputs: map[string]PortDefinition{"value": {Type: ValueTypeString}}},
		},
		Edges:   []EdgeDefinition{{From: "producer.outputs.value", To: "consumer.inputs.value"}},
		Outputs: map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	inputs, result := plan.ResolveNodeInputs("consumer", nil, map[string]map[string]Value{"producer": {}})
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
		edges []EdgeDefinition
		code  string
	}{
		{"source node", []EdgeDefinition{{From: "missing.outputs.value", To: "consumer.inputs.value"}}, "E_EDGE_SOURCE_NODE_NOT_FOUND"},
		{"target node", []EdgeDefinition{{From: "producer.outputs.value", To: "missing.inputs.value"}}, "E_EDGE_TARGET_NODE_NOT_FOUND"},
		{"source output", []EdgeDefinition{{From: "producer.outputs.missing", To: "consumer.inputs.value"}}, "E_EDGE_SOURCE_OUTPUT_NOT_FOUND"},
		{"target input", []EdgeDefinition{{From: "producer.outputs.value", To: "consumer.inputs.missing"}}, "E_EDGE_TARGET_INPUT_NOT_FOUND"},
		{"duplicate provider", []EdgeDefinition{
			{From: "producer.outputs.value", To: "consumer.inputs.value"},
			{From: "producer.outputs.value", To: "consumer.inputs.value"},
		}, "E_INPUT_MULTIPLE_PROVIDERS"},
		{"wrong direction", []EdgeDefinition{{From: "producer.inputs.value", To: "consumer.inputs.value"}}, "E_EDGE_SOURCE_REFERENCE_INVALID"},
		{"no binding", []EdgeDefinition{}, "E_REQUIRED_INPUT_UNBOUND"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			definition := &Definition{
				SchemaVersion: SchemaVersionV0,
				ID:            "invalid-binding",
				Inputs:        map[string]InputDefinition{},
				Nodes: map[string]NodeDefinition{
					"producer": {Type: "shell", Outputs: map[string]PortDefinition{"value": {Type: ValueTypeString}}},
					"consumer": {Type: "shell", Inputs: map[string]PortDefinition{"value": {Type: ValueTypeString, Required: true}}},
				},
				Edges:   test.edges,
				Outputs: map[string]OutputDefinition{},
			}
			plan, result := CompileBindings(definition)
			if plan != nil || result.Valid() {
				t.Fatalf("CompileBindings() = (%#v, %#v), want no plan", plan, result)
			}
			assertDiagnostic(t, result, test.code, "")
		})
	}
}

func TestStructuredJSONBindingPreservesValueWithoutSharingBytes(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "json-binding",
		Inputs:        map[string]InputDefinition{"payload": {Type: ValueTypeJSON}},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{"payload": {Type: ValueTypeJSON}}},
		},
		Edges:              []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.payload", To: "worker.inputs.payload"}},
		Outputs:            map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	raw := json.RawMessage(`{"items":[1,true,{"name":"ready"}]}`)
	value, err := NewJSONValue(raw)
	if err != nil {
		t.Fatalf("NewJSONValue() error = %v", err)
	}
	raw[0] = '['
	inputs, result := plan.ResolveNodeInputs("worker", map[string]Value{"payload": value}, nil)
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
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "artifact-binding",
		Inputs:        map[string]InputDefinition{"report": {Type: ValueTypeArtifactRef}},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{"report": {Type: ValueTypeArtifactRef, Required: true}}},
		},
		Edges:              []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.report", To: "worker.inputs.report"}},
		Outputs:            map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	value, err := NewArtifactRefValue("artifact:does-not-exist")
	if err != nil {
		t.Fatalf("NewArtifactRefValue() error = %v", err)
	}
	inputs, result := plan.ResolveNodeInputs("worker", map[string]Value{"report": value}, nil)
	if !result.Valid() {
		t.Fatalf("ResolveNodeInputs() diagnostics = %#v", result.Diagnostics)
	}
	if got, ok := inputs["report"].AsArtifactRef(); !ok || got != "artifact:does-not-exist" {
		t.Fatalf("artifact reference = (%q, %v), want opaque handle", got, ok)
	}
}

func TestOptionalGraphInputCanRemainUnset(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "optional-input",
		Inputs:        map[string]InputDefinition{"note": {Type: ValueTypeString}},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{"note": {Type: ValueTypeString}}},
		},
		Edges:              []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.note", To: "worker.inputs.note"}},
		Outputs:            map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	inputs, result := plan.ResolveNodeInputs("worker", nil, nil)
	if !result.Valid() || len(inputs) != 0 {
		t.Fatalf("optional ResolveNodeInputs() = (%#v, %#v), want empty valid inputs", inputs, result)
	}
}

func TestMissingRequiredGraphInputBlocksOptionalTargetResolution(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "required-source-optional-target",
		Inputs:        map[string]InputDefinition{"note": {Type: ValueTypeString, Required: true}},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{"note": {Type: ValueTypeString}}},
		},
		Edges:              []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.note", To: "worker.inputs.note"}},
		Outputs:            map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
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
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "warning-only",
		Inputs:        map[string]InputDefinition{},
		Nodes: map[string]NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]PortDefinition{"unused": {Type: ValueTypeString}}},
		},
		Edges:   []EdgeDefinition{},
		Outputs: map[string]OutputDefinition{},
	}
	plan, result := CompileBindings(definition)
	if plan == nil || !result.Valid() {
		t.Fatalf("warning-only CompileBindings() = (%#v, %#v), want plan", plan, result)
	}
	assertDiagnostic(t, result, "W_DECLARED_OUTPUT_UNUSED", "nodes.producer.outputs.unused")

	definition.Nodes["consumer"] = NodeDefinition{Type: "shell", Inputs: map[string]PortDefinition{"count": {Type: ValueTypeNumber}}}
	definition.Edges = []EdgeDefinition{{From: "producer.outputs.unused", To: "consumer.inputs.count"}}
	plan, result = CompileBindings(definition)
	if plan != nil || result.Valid() {
		t.Fatalf("mismatched CompileBindings() = (%#v, %#v), want blocking error", plan, result)
	}
	assertDiagnostic(t, result, "E_BINDING_TYPE_MISMATCH", "edges[0]")
}

func TestBindingResolutionAndDiagnosticsAreDeterministic(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "deterministic-bindings",
		Inputs: map[string]InputDefinition{
			"first":  {Type: ValueTypeString},
			"second": {Type: ValueTypeString},
		},
		Nodes: map[string]NodeDefinition{
			"worker": {Type: "shell", Inputs: map[string]PortDefinition{
				"first":  {Type: ValueTypeString, Required: true},
				"second": {Type: ValueTypeString, Required: true},
			}},
		},
		Edges: []EdgeDefinition{},
		GraphInputBindings: []GraphInputBindingDefinition{
			{From: "inputs.second", To: "worker.inputs.second"},
			{From: "inputs.first", To: "worker.inputs.first"},
		},
		Outputs: map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	firstValues := map[string]Value{"first": NewStringValue("A"), "second": NewStringValue("B")}
	secondValues := make(map[string]Value)
	secondValues["second"] = NewStringValue("B")
	secondValues["first"] = NewStringValue("A")
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
	var plan *BindingPlan
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

func TestUnboundNodesHaveNoDependencies(t *testing.T) {
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "unbound-nodes",
		Inputs:        map[string]InputDefinition{},
		Nodes: map[string]NodeDefinition{
			"left":  {Type: "shell"},
			"right": {Type: "shell"},
		},
		Edges:   []EdgeDefinition{},
		Outputs: map[string]OutputDefinition{},
	}
	plan, result := CompileBindings(definition)
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
	data, err := os.ReadFile("../../.agents/graphs/hello-parallel.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	definition, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	plan, result := CompileBindings(definition)
	if !result.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", result.Diagnostics)
	}
	definition.Edges = nil
	definition.Nodes["merge"] = NodeDefinition{Type: "shell"}
	dependencies, err := plan.Dependencies("merge")
	if err != nil || !reflect.DeepEqual(dependencies, []string{"left", "right"}) {
		t.Fatalf("Dependencies(merge) = (%v, %v), want left/right", dependencies, err)
	}
	dependencies[0] = "mutated"
	again, _ := plan.Dependencies("merge")
	if !reflect.DeepEqual(again, []string{"left", "right"}) {
		t.Fatalf("Dependencies() leaked mutable slice: %v", again)
	}
	inputs, resolution := plan.ResolveNodeInputs("merge", nil, map[string]map[string]Value{
		"left":  {"stdout": NewStringValue("L")},
		"right": {"stdout": NewStringValue("R")},
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
	definition := &Definition{
		SchemaVersion: SchemaVersionV0,
		ID:            "runtime-errors",
		Inputs:        map[string]InputDefinition{},
		Nodes: map[string]NodeDefinition{
			"producer": {Type: "shell", Outputs: map[string]PortDefinition{"value": {Type: ValueTypeString}}},
			"consumer": {Type: "shell", Inputs: map[string]PortDefinition{"value": {Type: ValueTypeString}}},
		},
		Edges:   []EdgeDefinition{{From: "producer.outputs.value", To: "consumer.inputs.value"}},
		Outputs: map[string]OutputDefinition{},
	}
	plan, diagnostics := CompileBindings(definition)
	if !diagnostics.Valid() {
		t.Fatalf("CompileBindings() diagnostics = %#v", diagnostics.Diagnostics)
	}
	assertDiagnostic(t, plan.ValidateGraphInputs(map[string]Value{"typo": NewStringValue("unexpected")}), "E_GRAPH_INPUT_VALUE_UNKNOWN", "inputs.typo")
	inputs, result := plan.ResolveNodeInputs("consumer", nil, map[string]map[string]Value{
		"producer": {"value": NewBooleanValue(true)},
	})
	if inputs != nil || result.Valid() {
		t.Fatalf("ResolveNodeInputs() = (%#v, %#v), want blocking type error", inputs, result)
	}
	diagnostic := assertDiagnostic(t, result, "E_BOUND_VALUE_TYPE_MISMATCH", "nodes.consumer.inputs.value")
	if diagnostic.Location.EdgeIndex == nil || *diagnostic.Location.EdgeIndex != 0 {
		t.Fatalf("type error location = %#v, want edge 0", diagnostic.Location)
	}
}
