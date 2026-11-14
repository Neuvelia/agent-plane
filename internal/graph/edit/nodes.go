package edit

import (
	"fmt"

	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

// nodeTypes are the node types the editor can create.
var nodeTypes = map[string]bool{"shell": true, "agent": true, "gate": true, "human": true}

// starter is the shape of a new node: valid YAML, possibly semantically
// incomplete (the validator says what is missing). One definition feeds both the
// tree and the typed prediction.
func starter(nodeType string) graph.NodeDefinition {
	switch nodeType {
	case "shell":
		return graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": ""}}
	case "agent":
		return graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"adapter": "", "workspace": "", "task": ""}}
	case "gate":
		return graph.NodeDefinition{Type: "gate", Config: graph.NodeConfig{"check_id": "", "kind": "json_predicate"}}
	default: // human
		return graph.NodeDefinition{
			Type:    "human",
			Config:  graph.NodeConfig{"prompt": ""},
			Outputs: map[string]graph.PortDefinition{"decision": {Type: graph.ValueTypeString, Required: true}},
		}
	}
}

func nodeError(code, id, message, suggestion string) *Error {
	return &Error{Code: code, Message: message, OpIndex: -1, Node: id, Suggestion: suggestion}
}

func (s *session) nodesMapping() (*yaml.Node, *Error) {
	nodes, i := get(s.top, "nodes")
	if i < 0 {
		nodes = mapNode(false)
		insertTop(s.top, "nodes", nodes)
		return nodes, nil
	}
	if nodes.Kind != yaml.MappingNode {
		return nil, nodeError(CodeParse, "", "nodes is not a mapping", "fix the YAML text")
	}
	if len(nodes.Content) == 0 {
		nodes.Style &^= yaml.FlowStyle
	}
	return nodes, nil
}

func (s *session) addNode(op Op) *Error {
	id := op.ID
	if !graph.ValidIdentifier(id) {
		return nodeError(CodeNodeID, id, fmt.Sprintf("%q is not a valid node id", id), "start with a lowercase letter, then lowercase letters, digits, - or _")
	}
	if !nodeTypes[op.Type] {
		return nodeError(CodeNodeType, id, fmt.Sprintf("cannot create a node of type %q", op.Type), "use shell, agent, gate or human")
	}
	if _, exists := s.want.Nodes[id]; exists {
		return nodeError(CodeNodeExists, id, fmt.Sprintf("node %q already exists", id), "choose another id")
	}
	var x, y int
	if op.Layout != nil {
		var ok bool
		if x, y, ok = op.Layout.round(); !ok {
			return nodeError(CodeLayout, id, "layout coordinates must be finite numbers within -100000..100000", "")
		}
	}

	def := starter(op.Type)
	// typed prediction
	if s.want.Nodes == nil {
		s.want.Nodes = map[string]graph.NodeDefinition{}
	}
	s.want.Nodes[id] = def
	// tree
	nodes, err := s.nodesMapping()
	if err != nil {
		return err
	}
	var value yaml.Node
	if e := value.Encode(def); e != nil {
		return nodeError(CodeParse, id, "cannot build the node: "+e.Error(), "")
	}
	nodes.Content = append(nodes.Content, str(id), &value)

	if op.Layout != nil {
		return s.putLayout(id, x, y)
	}
	return nil
}

func (s *session) removeNode(op Op) *Error {
	id := op.ID
	if _, ok := s.want.Nodes[id]; !ok {
		return nodeError(CodeNodeNotFound, id, fmt.Sprintf("node %q does not exist", id), "")
	}
	// typed prediction
	delete(s.want.Nodes, id)
	s.want.Edges = filter(s.want.Edges, func(e graph.EdgeDefinition) bool {
		return !hasNodePrefix(string(e.From), id) && !hasNodePrefix(string(e.To), id)
	})
	s.want.GraphInputBindings = filter(s.want.GraphInputBindings, func(b graph.GraphInputBindingDefinition) bool { return !hasNodePrefix(string(b.To), id) })
	for name, out := range s.want.Outputs {
		if hasNodePrefix(string(out.From), id) {
			delete(s.want.Outputs, name)
		}
	}
	s.want.Units = filter(s.want.Units, func(u string) bool { return u != id })
	if len(s.want.Units) == 0 {
		s.want.Units = nil
	}
	typedLayoutRemove(s.want, id)

	// tree
	if nodes, i := get(s.top, "nodes"); i >= 0 {
		remove(nodes, id)
	}
	s.dropSeqItems("edges", func(item *yaml.Node) bool {
		from, _ := get(item, "from")
		to, _ := get(item, "to")
		return (from != nil && hasNodePrefix(from.Value, id)) || (to != nil && hasNodePrefix(to.Value, id))
	})
	s.dropSeqItems("graph_input_bindings", func(item *yaml.Node) bool {
		to, _ := get(item, "to")
		return to != nil && hasNodePrefix(to.Value, id)
	})
	if outs, i := get(s.top, "outputs"); i >= 0 && outs.Kind == yaml.MappingNode {
		for j := 0; j+1 < len(outs.Content); {
			if from, _ := get(outs.Content[j+1], "from"); from != nil && hasNodePrefix(from.Value, id) {
				outs.Content = append(outs.Content[:j], outs.Content[j+2:]...)
				continue
			}
			j += 2
		}
	}
	if units, i := get(s.top, "units"); i >= 0 && units.Kind == yaml.SequenceNode {
		kept := units.Content[:0]
		for _, u := range units.Content {
			if u.Value != id {
				kept = append(kept, u)
			}
		}
		units.Content = kept
		if len(units.Content) == 0 {
			remove(s.top, "units") // an empty units list is invalid
		}
	}
	s.treeLayoutRemove(id)
	return nil
}

func (s *session) renameNode(op Op) *Error {
	id, to := op.ID, op.NewID
	node, ok := s.want.Nodes[id]
	if !ok {
		return nodeError(CodeNodeNotFound, id, fmt.Sprintf("node %q does not exist", id), "")
	}
	if !graph.ValidIdentifier(to) {
		return nodeError(CodeNodeID, to, fmt.Sprintf("%q is not a valid node id", to), "start with a lowercase letter, then lowercase letters, digits, - or _")
	}
	if _, exists := s.want.Nodes[to]; exists {
		return nodeError(CodeNodeExists, to, fmt.Sprintf("node %q already exists", to), "choose another id")
	}
	fix := func(ref string) string {
		if hasNodePrefix(ref, id) {
			return to + ref[len(id):]
		}
		return ref
	}
	// typed prediction
	delete(s.want.Nodes, id)
	s.want.Nodes[to] = node
	for i := range s.want.Edges {
		s.want.Edges[i].From = graph.NodeOutputReference(fix(string(s.want.Edges[i].From)))
		s.want.Edges[i].To = graph.NodeInputReference(fix(string(s.want.Edges[i].To)))
	}
	for i := range s.want.GraphInputBindings {
		s.want.GraphInputBindings[i].To = graph.NodeInputReference(fix(string(s.want.GraphInputBindings[i].To)))
	}
	for name, out := range s.want.Outputs {
		out.From = graph.NodeOutputReference(fix(string(out.From)))
		s.want.Outputs[name] = out
	}
	for i, u := range s.want.Units {
		if u == id {
			s.want.Units[i] = to
		}
	}
	typedLayoutRename(s.want, id, to)

	// tree
	if nodes, _ := get(s.top, "nodes"); nodes != nil {
		for i := 0; i+1 < len(nodes.Content); i += 2 {
			if nodes.Content[i].Value == id {
				nodes.Content[i].Value = to
			}
		}
	}
	renameRefs := func(seq string, keys ...string) {
		if items, i := get(s.top, seq); i >= 0 {
			for _, item := range items.Content {
				for _, k := range keys {
					if v, _ := get(item, k); v != nil {
						v.Value = fix(v.Value)
					}
				}
			}
		}
	}
	renameRefs("edges", "from", "to")
	renameRefs("graph_input_bindings", "to")
	if outs, i := get(s.top, "outputs"); i >= 0 && outs.Kind == yaml.MappingNode {
		for j := 1; j < len(outs.Content); j += 2 {
			if from, _ := get(outs.Content[j], "from"); from != nil {
				from.Value = fix(from.Value)
			}
		}
	}
	if units, i := get(s.top, "units"); i >= 0 {
		for _, u := range units.Content {
			if u.Value == id {
				u.Value = to
			}
		}
	}
	s.treeLayoutRename(id, to)
	s.renames = append(s.renames, [2]string{"nodes." + id, "nodes." + to}, [2]string{"metadata.layout.nodes." + id, "metadata.layout.nodes." + to})
	return nil
}

// dropSeqItems removes the items of a top-level sequence that match.
func (s *session) dropSeqItems(key string, match func(item *yaml.Node) bool) {
	seq, i := get(s.top, key)
	if i < 0 || seq.Kind != yaml.SequenceNode {
		return
	}
	kept := seq.Content[:0]
	for _, item := range seq.Content {
		if !match(item) {
			kept = append(kept, item)
		}
	}
	seq.Content = kept
}

func filter[T any](items []T, keep func(T) bool) []T {
	var out []T
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}

func (s *session) setName(op Op) *Error {
	name := *op.Name
	s.want.Name = name
	if name == "" {
		remove(s.top, "name")
		return nil
	}
	if v, i := get(s.top, "name"); i >= 0 {
		v.Kind, v.Tag, v.Value = yaml.ScalarNode, "!!str", name
		return nil
	}
	insertTop(s.top, "name", str(name))
	return nil
}
