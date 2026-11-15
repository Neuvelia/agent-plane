package graph

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// referenceIndependence is a deliberately different computation: the
// transitive closure of the edge relation by Warshall's algorithm, with node
// ids read from the reference text by splitting on dots.
func referenceIndependence(def *Definition) map[string][]string {
	ids := make([]string, 0, len(def.Nodes))
	for id := range def.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	index := map[string]int{}
	for i, id := range ids {
		index[id] = i
	}
	n := len(ids)
	path := make([][]bool, n)
	for i := range path {
		path[i] = make([]bool, n)
	}
	for _, e := range def.Edges {
		from := strings.Split(string(e.From), ".")
		to := strings.Split(string(e.To), ".")
		if len(from) != 3 || len(to) != 3 || from[1] != "outputs" || to[1] != "inputs" {
			continue
		}
		i, okI := index[from[0]]
		j, okJ := index[to[0]]
		if okI && okJ {
			path[i][j] = true
		}
	}
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if path[i][k] && path[k][j] {
					path[i][j] = true
				}
			}
		}
	}
	out := map[string][]string{}
	for i, id := range ids {
		list := []string{}
		for j, other := range ids {
			if i != j && !path[i][j] && !path[j][i] {
				list = append(list, other)
			}
		}
		out[id] = list
	}
	return out
}

func TestIndependenceEqualsTheReferenceForTheCorpusGraphs(t *testing.T) {
	files, err := filepath.Glob("../../.agents/graphs/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus graphs: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		def, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if got, want := IndependentNodes(def), referenceIndependence(def); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: IndependentNodes = %v, reference = %v", filepath.Base(f), got, want)
		}
	}
}

func TestIndependenceOfSmallShapes(t *testing.T) {
	node := func() NodeDefinition { return NodeDefinition{Type: "shell"} }
	edge := func(from, to string) EdgeDefinition {
		return EdgeDefinition{From: NodeOutputReference(from + ".outputs.o"), To: NodeInputReference(to + ".inputs.i")}
	}
	cases := map[string]struct {
		def  *Definition
		want map[string][]string
	}{
		"parallel branches join": {
			&Definition{Nodes: map[string]NodeDefinition{"left": node(), "right": node(), "merge": node()},
				Edges: []EdgeDefinition{edge("left", "merge"), edge("right", "merge")}},
			map[string][]string{"left": {"right"}, "right": {"left"}, "merge": {}},
		},
		"chain and an island": {
			&Definition{Nodes: map[string]NodeDefinition{"a": node(), "b": node(), "c": node(), "z": node()},
				Edges: []EdgeDefinition{edge("a", "b"), edge("b", "c")}},
			map[string][]string{"a": {"z"}, "b": {"z"}, "c": {"z"}, "z": {"a", "b", "c"}},
		},
		"cycle and broken references": {
			&Definition{Nodes: map[string]NodeDefinition{"a": node(), "b": node(), "c": node()},
				Edges: []EdgeDefinition{edge("a", "b"), edge("b", "a"), {From: "nope", To: "c.inputs.i"}, edge("ghost", "c")}},
			map[string][]string{"a": {"c"}, "b": {"c"}, "c": {"a", "b"}},
		},
		"no nodes": {&Definition{}, map[string][]string{}},
	}
	for name, c := range cases {
		got := IndependentNodes(c.def)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
		if !reflect.DeepEqual(got, referenceIndependence(c.def)) {
			t.Errorf("%s: differs from the reference", name)
		}
	}
	if got := IndependentNodes(nil); len(got) != 0 {
		t.Errorf("nil definition: %v", got)
	}
}
