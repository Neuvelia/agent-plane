package api

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func portNames(ports []GraphPort) []string {
	names := []string{}
	for _, p := range ports {
		names = append(names, p.Name)
	}
	return names
}

func TestSummarizeGraphReturnsDeclaredPortsAndBindingsOfTheDemoGraph(t *testing.T) {
	source, err := os.ReadFile("../../.agents/graphs/demo-vertical.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := summarizeGraph(source)
	nodes := map[string]GraphNode{}
	for _, n := range got.Nodes {
		nodes[n.ID] = n
	}
	if want := []string{"stdout"}; !reflect.DeepEqual(portNames(nodes["fast"].Outputs), want) {
		t.Fatalf("fast outputs = %+v", nodes["fast"].Outputs)
	}
	if out := nodes["approve"].Outputs; len(out) != 1 || out[0] != (GraphPort{Name: "decision", Type: "string", Required: true}) {
		t.Fatalf("approve outputs = %+v", out)
	}
	if in := nodes["apply"].Inputs; len(in) != 1 || in[0].Name != "decision" {
		t.Fatalf("apply inputs = %+v", in)
	}
	// Deterministic: sorted by target node, target input, then source.
	for i := 1; i < len(got.Bindings); i++ {
		if bindingLess(got.Bindings[i], got.Bindings[i-1]) {
			t.Fatalf("bindings not sorted: %+v", got.Bindings)
		}
	}
	want := GraphBinding{Source: GraphBindingSource{Node: "approve", Port: "decision"}, Target: GraphBindingTarget{Node: "apply", Input: "decision"}}
	found := false
	for _, b := range got.Bindings {
		found = found || b == want
	}
	if !found {
		t.Fatalf("missing %+v in %+v", want, got.Bindings)
	}
	again := summarizeGraph(source)
	if !reflect.DeepEqual(got, again) {
		t.Fatal("summary is not deterministic")
	}
}

func TestSummarizeGraphIncludesGraphInputBindingsAndOptionalPorts(t *testing.T) {
	source := "schema_version: 0\nid: g\ninputs:\n  job: {type: string, required: true}\nnodes:\n  a: {type: shell, inputs: {job: {type: string, required: true}, note: {type: string}}, outputs: {stdout: {type: string}}}\nedges: []\ngraph_input_bindings:\n  - {from: inputs.job, to: a.inputs.job}\noutputs: {}\n"
	got := summarizeGraph([]byte(source))
	want := []GraphBinding{{Source: GraphBindingSource{Input: "job"}, Target: GraphBindingTarget{Node: "a", Input: "job"}}}
	if !reflect.DeepEqual(got.Bindings, want) {
		t.Fatalf("bindings = %+v", got.Bindings)
	}
	in := got.Nodes[0].Inputs
	if len(in) != 2 || in[0] != (GraphPort{Name: "job", Type: "string", Required: true}) || in[1] != (GraphPort{Name: "note", Type: "string"}) {
		t.Fatalf("inputs = %+v", in)
	}
}

func TestSummarizeGraphOfAnUnparsableGraphHasEmptyCollectionsNotNull(t *testing.T) {
	got := summarizeGraph([]byte("not: [valid"))
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"nodes":[],"edges":[],"bindings":[]}` {
		t.Fatalf("summary = %s", raw)
	}
}

func TestSummarizeGraphPortsAreEmptyListsForANodeWithoutPorts(t *testing.T) {
	got := summarizeGraph([]byte("schema_version: 0\nid: g\ninputs: {}\nnodes:\n  a: {type: shell}\nedges: []\noutputs: {}\n"))
	raw, _ := json.Marshal(got.Nodes[0])
	if string(raw) != `{"id":"a","type":"shell","inputs":[],"outputs":[]}` {
		t.Fatalf("node = %s", raw)
	}
}
