package api

import (
	"sort"
	"strings"

	"agentplane/internal/domain/graph"
)

// GraphPort is a declared node input or output: names and types only, never values.
type GraphPort struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// GraphBinding is one port-level data dependency. Exactly one of Source.Node
// (with Port) and Source.Input is set: a node output or a graph input name.
type GraphBinding struct {
	Source GraphBindingSource `json:"source"`
	Target GraphBindingTarget `json:"target"`
}

// GraphBindingSource is where a bound value comes from.
type GraphBindingSource struct {
	Node  string `json:"node,omitempty"`
	Port  string `json:"port,omitempty"`
	Input string `json:"input,omitempty"`
}

// GraphBindingTarget is the node input that receives it.
type GraphBindingTarget struct {
	Node  string `json:"node"`
	Input string `json:"input"`
}

// declaredPorts lists ports sorted by name, never nil so the JSON is `[]`.
func declaredPorts(ports map[string]graph.PortDefinition) []GraphPort {
	out := make([]GraphPort, 0, len(ports))
	for name, p := range ports {
		out = append(out, GraphPort{Name: name, Type: string(p.Type), Required: p.Required})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// summarizeBindings returns node-to-node and graph-input bindings sorted by
// target node, target input, then source, so the order is deterministic.
// References are `node.outputs.port`, `inputs.NAME` and `node.inputs.name`.
func summarizeBindings(def *graph.Definition) []GraphBinding {
	out := []GraphBinding{}
	for _, e := range def.Edges {
		from := strings.SplitN(string(e.From), ".", 3)
		to := strings.SplitN(string(e.To), ".", 3)
		if len(from) == 3 && len(to) == 3 {
			out = append(out, GraphBinding{Source: GraphBindingSource{Node: from[0], Port: from[2]}, Target: GraphBindingTarget{Node: to[0], Input: to[2]}})
		}
	}
	for _, b := range def.GraphInputBindings {
		from := strings.SplitN(string(b.From), ".", 2)
		to := strings.SplitN(string(b.To), ".", 3)
		if len(from) == 2 && len(to) == 3 {
			out = append(out, GraphBinding{Source: GraphBindingSource{Input: from[1]}, Target: GraphBindingTarget{Node: to[0], Input: to[2]}})
		}
	}
	sort.Slice(out, func(i, j int) bool { return bindingLess(out[i], out[j]) })
	return out
}

func bindingLess(a, b GraphBinding) bool {
	if a.Target != b.Target {
		if a.Target.Node != b.Target.Node {
			return a.Target.Node < b.Target.Node
		}
		return a.Target.Input < b.Target.Input
	}
	if a.Source.Input != b.Source.Input {
		return a.Source.Input < b.Source.Input
	}
	if a.Source.Node != b.Source.Node {
		return a.Source.Node < b.Source.Node
	}
	return a.Source.Port < b.Source.Port
}
