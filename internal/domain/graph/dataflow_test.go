package graph

import (
	"reflect"
	"sort"
	"testing"
)

func shellNode(inputs, outputs map[string]PortDefinition) NodeDefinition {
	return NodeDefinition{Type: "shell", Config: NodeConfig{"command": "/bin/true"}, Inputs: inputs, Outputs: outputs}
}

func port(valueType ValueType) PortDefinition { return PortDefinition{Type: valueType} }

func requiredPort(valueType ValueType) PortDefinition {
	return PortDefinition{Type: valueType, Required: true}
}

// dataflowCorpus covers every way a connection can add a diagnostic: types,
// providers, graph input bindings, required inputs, cycles, secret flow and
// broken references already in the graph.
func dataflowCorpus() map[string]*Definition {
	str, num := port(ValueTypeString), port(ValueTypeNumber)
	return map[string]*Definition{
		"chain": {
			ID: "chain", Inputs: map[string]InputDefinition{"seed": {Type: ValueTypeString}, "count": {Type: ValueTypeNumber}},
			Nodes: map[string]NodeDefinition{
				"a": shellNode(map[string]PortDefinition{"i": str}, map[string]PortDefinition{"o": str, "n": num}),
				"b": shellNode(map[string]PortDefinition{"i": str, "k": num}, map[string]PortDefinition{"o": str}),
				"c": shellNode(map[string]PortDefinition{"i": requiredPort(ValueTypeString)}, map[string]PortDefinition{"o": str}),
			},
			Edges:   []EdgeDefinition{{From: "a.outputs.o", To: "b.inputs.i"}},
			Outputs: map[string]OutputDefinition{"result": {From: "c.outputs.o"}},
		},
		"providers-and-bindings": {
			ID: "providers", Inputs: map[string]InputDefinition{"seed": {Type: ValueTypeString}},
			Nodes: map[string]NodeDefinition{
				"a": shellNode(nil, map[string]PortDefinition{"o": str}),
				"b": shellNode(map[string]PortDefinition{"i": str, "j": str}, map[string]PortDefinition{"o": str}),
			},
			Edges:              []EdgeDefinition{{From: "a.outputs.o", To: "b.inputs.i"}},
			GraphInputBindings: []GraphInputBindingDefinition{{From: "inputs.seed", To: "b.inputs.j"}},
			Outputs:            map[string]OutputDefinition{},
		},
		"already-broken": {
			ID: "broken", Inputs: map[string]InputDefinition{},
			Nodes: map[string]NodeDefinition{
				"a": shellNode(map[string]PortDefinition{"i": requiredPort(ValueTypeString)}, map[string]PortDefinition{"o": str, "x": port("weird")}),
				"b": shellNode(map[string]PortDefinition{"i": num, "w": port("weird")}, map[string]PortDefinition{"o": str}),
			},
			Edges: []EdgeDefinition{
				{From: "a.outputs.o", To: "b.inputs.i"},
				{From: "ghost.outputs.o", To: "b.inputs.i"},
				{From: "not a reference", To: "a.inputs.i"},
				{From: "b.outputs.o", To: "a.inputs.i"},
			},
			Outputs: map[string]OutputDefinition{},
		},
		"secret-flow": {
			ID: "secrets", Inputs: map[string]InputDefinition{},
			Secrets: map[string]SecretDefinition{"tok": {SecretRef: "env/TOKEN"}},
			Nodes: map[string]NodeDefinition{
				"holder": {Type: "shell", Config: NodeConfig{"command": "/bin/true", secretEnvKey: map[string]string{"TOKEN": "tok"}}, Outputs: map[string]PortDefinition{"o": str}},
				"relay":  shellNode(map[string]PortDefinition{"i": str}, map[string]PortDefinition{"o": str}),
				"agent":  {Type: "agent", Config: NodeConfig{"task": "do it"}, Inputs: map[string]PortDefinition{"i": str, "j": str}, Outputs: map[string]PortDefinition{"summary": str}},
			},
			Edges:   []EdgeDefinition{{From: "holder.outputs.o", To: "relay.inputs.i"}},
			Outputs: map[string]OutputDefinition{},
		},
		"no-edges-collection": {
			ID: "bare", Inputs: map[string]InputDefinition{"seed": {Type: ValueTypeString}},
			Nodes: map[string]NodeDefinition{
				"a": shellNode(nil, map[string]PortDefinition{"o": str}),
				"b": shellNode(map[string]PortDefinition{"i": requiredPort(ValueTypeString)}, nil),
			},
			Outputs: map[string]OutputDefinition{},
		},
	}
}

// connections lists every edge from a node output and every binding from a
// graph input to a node input, plus references the validator must refuse.
func connections(def *Definition) []*Definition {
	var out []*Definition
	addEdge := func(from, to string) {
		after := *def
		after.Edges = append(append([]EdgeDefinition(nil), def.Edges...), EdgeDefinition{From: NodeOutputReference(from), To: NodeInputReference(to)})
		out = append(out, &after)
	}
	addBinding := func(from, to string) {
		after := *def
		after.GraphInputBindings = append(append([]GraphInputBindingDefinition(nil), def.GraphInputBindings...), GraphInputBindingDefinition{From: GraphInputReference(from), To: NodeInputReference(to)})
		out = append(out, &after)
	}
	var targets []string
	for _, id := range sortedNodeIDs(def.Nodes) {
		for _, in := range sortedPortIDs(def.Nodes[id].Inputs) {
			targets = append(targets, id+".inputs."+in)
		}
	}
	targets = append(targets, "ghost.inputs.i", "a.inputs.missing", "bad ref")
	for _, to := range targets {
		for _, id := range sortedNodeIDs(def.Nodes) {
			for _, o := range sortedPortIDs(def.Nodes[id].Outputs) {
				addEdge(id+".outputs."+o, to)
			}
		}
		addEdge("ghost.outputs.o", to)
		for _, in := range sortedInputIDs(def.Inputs) {
			addBinding("inputs."+in, to)
		}
		addBinding("inputs.missing", to)
	}
	return out
}

// addedErrors is what an editor reports for a connection: the blocking
// diagnostics of after that before does not have, matched by code and message.
func addedErrors(validate func(*Definition) ValidationResult, before, after *Definition) []Diagnostic {
	left := map[string]int{}
	for _, d := range validate(before).Diagnostics {
		if d.Severity == SeverityError {
			left[d.Code+"|"+d.Message]++
		}
	}
	var added []Diagnostic
	for _, d := range validate(after).Diagnostics {
		key := d.Code + "|" + d.Message
		switch {
		case d.Severity != SeverityError:
		case left[key] > 0:
			left[key]--
		default:
			added = append(added, d)
		}
	}
	return added
}

func TestValidateDataflowMatchesValidateForConnectionEdits(t *testing.T) {
	corpus := dataflowCorpus()
	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		def := corpus[name]
		for _, after := range connections(def) {
			full := addedErrors(Validate, def, after)
			subset := addedErrors(ValidateDataflow, def, after)
			for _, d := range full {
				seen[d.Code] = true
			}
			if !reflect.DeepEqual(full, subset) {
				t.Errorf("%s: edges %v bindings %v:\nValidate added         %+v\nValidateDataflow added %+v",
					name, after.Edges[len(def.Edges):], after.GraphInputBindings[len(def.GraphInputBindings):], full, subset)
			}
		}
	}
	// The corpus must exercise every kind of error a connection can add.
	for _, code := range []string{"E_BINDING_TYPE_MISMATCH", "E_INPUT_MULTIPLE_PROVIDERS", "E_GRAPH_CYCLE", "E_SECRET_FLOWS_TO_AGENT",
		"E_EDGE_SOURCE_NODE_NOT_FOUND", "E_EDGE_TARGET_NODE_NOT_FOUND", "E_EDGE_TARGET_INPUT_NOT_FOUND", "E_EDGE_TARGET_REFERENCE_INVALID",
		"E_GRAPH_INPUT_NOT_FOUND", "E_GRAPH_INPUT_TARGET_NODE_NOT_FOUND", "E_GRAPH_INPUT_TARGET_REFERENCE_INVALID"} {
		if !seen[code] {
			t.Errorf("no connection in the corpus adds %s", code)
		}
	}
}

func TestValidateDataflowReportsValidatesDataflowDiagnosticsOnly(t *testing.T) {
	dataflowCodes := map[string]bool{}
	for _, def := range dataflowCorpus() {
		for _, d := range ValidateDataflow(def).Diagnostics {
			dataflowCodes[d.Code] = true
		}
	}
	for name, def := range dataflowCorpus() {
		var fromValidate []Diagnostic
		for _, d := range Validate(def).Diagnostics {
			if dataflowCodes[d.Code] {
				fromValidate = append(fromValidate, d)
			}
		}
		if got := ValidateDataflow(def).Diagnostics; !reflect.DeepEqual(got, fromValidate) {
			t.Errorf("%s: ValidateDataflow %+v, Validate's dataflow diagnostics %+v", name, got, fromValidate)
		}
	}
}

func TestValidateDataflowRejectsNilDefinition(t *testing.T) {
	got := ValidateDataflow(nil).Diagnostics
	if len(got) != 1 || got[0].Code != "E_DEFINITION_NIL" {
		t.Fatalf("ValidateDataflow(nil) = %+v", got)
	}
}

func TestSplitReferenceMatchesThreePartSplit(t *testing.T) {
	for _, ref := range []string{"a.outputs.o", "a.inputs.i", "a.outputs", "a.outputs.o.x", ".outputs.o", "a..o", "", "a", "a.b.c.d"} {
		first, middle, last, ok := splitReference(ref)
		want := splitThree(ref)
		if ok != (want != nil) || (ok && (first != want[0] || middle != want[1] || last != want[2])) {
			t.Errorf("splitReference(%q) = %q %q %q %v, want %v", ref, first, middle, last, ok, want)
		}
	}
}

func splitThree(ref string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(ref); i++ {
		if ref[i] == '.' {
			parts = append(parts, ref[start:i])
			start = i + 1
		}
	}
	parts = append(parts, ref[start:])
	if len(parts) != 3 {
		return nil
	}
	return parts
}
