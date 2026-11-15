package edit

import (
	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

// portRef names one port reference in both spellings it takes: the graph's
// NODE.SECTION.PORT and the loop binding's candidate.outputs.PORT or
// node.inputs.PORT.
type portRef struct {
	id, direction string
	graphRef      string
	loopRef       string
}

func newPortRef(id, direction, port string) portRef {
	r := portRef{id: id, direction: direction, graphRef: id + "." + portSection(direction) + "." + port}
	if direction == DirectionInput {
		r.loopRef = "node.inputs." + port
	} else {
		r.loopRef = "candidate.outputs." + port
	}
	return r
}

// rewritePortRefs renames every reference to a node's port to to, or removes
// it when to is empty: edges, graph input bindings and graph outputs, the
// node's loop bindings, and the node's own config entries that name one of its
// inputs (Shell input_env, human context_inputs, Gate left_input, right_input
// and input). Both the typed prediction and the tree are changed.
func (s *session) rewritePortRefs(id, direction, from, to string) {
	old := newPortRef(id, direction, from)
	var repl portRef
	if to != "" {
		repl = newPortRef(id, direction, to)
	}
	s.typedPortRefs(old, repl, from, to)
	s.treePortRefs(old, repl, from, to)
}

func (s *session) typedPortRefs(old, repl portRef, from, to string) {
	keep := to != ""
	if old.direction == DirectionInput {
		s.want.Edges = rewriteSlice(s.want.Edges, func(e *graph.EdgeDefinition) *graph.NodeInputReference { return &e.To }, old.graphRef, repl.graphRef, keep)
		s.want.GraphInputBindings = rewriteSlice(s.want.GraphInputBindings, func(b *graph.GraphInputBindingDefinition) *graph.NodeInputReference { return &b.To }, old.graphRef, repl.graphRef, keep)
	} else {
		s.want.Edges = rewriteSlice(s.want.Edges, func(e *graph.EdgeDefinition) *graph.NodeOutputReference { return &e.From }, old.graphRef, repl.graphRef, keep)
		for name, out := range s.want.Outputs {
			if string(out.From) != old.graphRef {
				continue
			}
			if keep {
				out.From = graph.NodeOutputReference(repl.graphRef)
				s.want.Outputs[name] = out
			} else {
				delete(s.want.Outputs, name)
			}
		}
	}
	n := s.want.Nodes[old.id]
	if n.Loop != nil {
		loop := *n.Loop
		loop.Bindings = rewriteSlice(loop.Bindings, func(b *graph.LoopBinding) *string { return &b.From }, old.loopRef, repl.loopRef, keep)
		n.Loop = &loop
	}
	if old.direction == DirectionInput && n.Config != nil {
		typedConfigRefs(n.Type, n.Config, from, to)
	}
	s.want.Nodes[old.id] = n
}

// rewriteSlice renames the field at() of matching items, or drops them.
func rewriteSlice[T any, R ~string](items []T, at func(*T) *R, old, repl string, keep bool) []T {
	out := items[:0:0]
	for i := range items {
		item := items[i]
		if string(*at(&item)) == old {
			if !keep {
				continue
			}
			*at(&item) = R(repl)
		}
		out = append(out, item)
	}
	if len(out) == 0 && items == nil {
		return nil
	}
	return out
}

func typedConfigRefs(nodeType string, cfg graph.NodeConfig, from, to string) {
	switch nodeType {
	case "shell":
		m, ok := asMapping(cfg["input_env"])
		if !ok {
			return
		}
		if v, present := m[from]; present {
			delete(m, from)
			if to != "" {
				m[to] = v
			}
		}
		if len(m) == 0 {
			delete(cfg, "input_env")
		}
	case "human":
		items, ok := cfg["context_inputs"].([]any)
		if !ok {
			return
		}
		kept := []any{}
		for _, it := range items {
			if it == from {
				if to == "" {
					continue
				}
				it = to
			}
			kept = append(kept, it)
		}
		cfg["context_inputs"] = kept
		if len(kept) == 0 {
			delete(cfg, "context_inputs")
		}
	case "gate":
		for _, k := range []string{"left_input", "right_input", "input"} {
			if cfg[k] == from {
				if to == "" {
					delete(cfg, k)
				} else {
					cfg[k] = to
				}
			}
		}
	}
}

func (s *session) treePortRefs(old, repl portRef, from, to string) {
	keep := to != ""
	field := func(item *yaml.Node, key string) *yaml.Node { v, _ := get(item, key); return v }
	rewrite := func(seqKey, key string) {
		if keep {
			if seq, _ := get(s.top, seqKey); seq != nil {
				for _, item := range seq.Content {
					if v := field(item, key); v != nil && v.Value == old.graphRef {
						v.Value = repl.graphRef
					}
				}
			}
			return
		}
		s.dropSeqItems(seqKey, func(item *yaml.Node) bool { v := field(item, key); return v != nil && v.Value == old.graphRef })
	}
	if old.direction == DirectionInput {
		rewrite("edges", "to")
		rewrite("graph_input_bindings", "to")
	} else {
		rewrite("edges", "from")
		if outs, _ := get(s.top, "outputs"); outs != nil && outs.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(outs.Content); {
				if v := field(outs.Content[j+1], "from"); v != nil && v.Value == old.graphRef {
					if !keep {
						outs.Content = append(outs.Content[:j], outs.Content[j+2:]...)
						continue
					}
					v.Value = repl.graphRef
				}
				j += 2
			}
		}
	}
	node, err := s.nodeTree(old.id)
	if err != nil {
		return // the typed prediction still changed; the safety net reports the mismatch
	}
	if loop, _ := get(node, "loop"); loop != nil {
		if seq, _ := get(loop, "bindings"); seq != nil && seq.Kind == yaml.SequenceNode {
			kept := seq.Content[:0]
			for _, item := range seq.Content {
				if v := field(item, "from"); v != nil && v.Value == old.loopRef {
					if !keep {
						continue
					}
					v.Value = repl.loopRef
				}
				kept = append(kept, item)
			}
			seq.Content = kept
		}
	}
	if old.direction == DirectionInput {
		typ, _ := get(node, "type")
		cfg, _ := get(node, "config")
		if typ != nil && cfg != nil && cfg.Kind == yaml.MappingNode {
			treeConfigRefs(typ.Value, cfg, from, to)
		}
	}
}

func treeConfigRefs(nodeType string, cfg *yaml.Node, from, to string) {
	switch nodeType {
	case "shell":
		m, _ := get(cfg, "input_env")
		if m == nil || m.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == from {
				if to == "" {
					m.Content = append(m.Content[:i], m.Content[i+2:]...)
				} else {
					m.Content[i].Value = to
				}
				break
			}
		}
		if len(m.Content) == 0 {
			remove(cfg, "input_env")
		}
	case "human":
		seq, _ := get(cfg, "context_inputs")
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return
		}
		kept := seq.Content[:0]
		for _, it := range seq.Content {
			if it.Kind == yaml.ScalarNode && it.Value == from {
				if to == "" {
					continue
				}
				it.Value = to
			}
			kept = append(kept, it)
		}
		seq.Content = kept
		if len(seq.Content) == 0 {
			remove(cfg, "context_inputs")
		}
	case "gate":
		for _, k := range []string{"left_input", "right_input", "input"} {
			if v, _ := get(cfg, k); v != nil && v.Kind == yaml.ScalarNode && v.Value == from {
				if to == "" {
					remove(cfg, k)
				} else {
					v.Value = to
				}
			}
		}
	}
}
