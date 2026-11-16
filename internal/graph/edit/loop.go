package edit

import (
	"fmt"
	"sort"
	"strings"

	"agentplane/internal/graph"

	"go.yaml.in/yaml/v3"
)

// attemptBounds are the loop budget bounds graph.LoopFields describes.
func attemptBounds() (int, int) {
	f := graph.LoopFields()[0]
	return *f.Min, *f.Max
}

func loopError(id, message, suggestion string) *Error {
	e := nodeError(CodeLoopInvalid, id, message, suggestion)
	e.Path = "nodes." + id + ".loop"
	return e
}

func (s *session) setLoop(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	if !graph.LoopProducer(n.Type) {
		return loopError(op.Node, fmt.Sprintf("a %s node cannot have a loop", n.Type), "only Shell and Agent nodes loop")
	}
	if lo, hi := attemptBounds(); op.MaxAttempts != nil && (*op.MaxAttempts < lo || *op.MaxAttempts > hi) {
		return loopError(op.Node, fmt.Sprintf("max_attempts must be from %d to %d", lo, hi), "")
	}
	cfgNode, cfgTyped, verr := decodeValue(op.Gate.Config)
	cfg, isObject := asMapping(cfgTyped)
	if verr != nil || !isObject {
		return loopError(op.Node, "the gate config must be a JSON object", "send {\"check_id\": ..., \"kind\": ...}")
	}
	inputs := map[string]graph.PortDefinition{}
	for name, p := range op.Gate.Inputs {
		if !graph.ValidIdentifier(name) || !valueTypes[graph.ValueType(p.Type)] {
			return loopError(op.Node, fmt.Sprintf("gate input %q must have a valid name and type", name), "")
		}
		inputs[name] = graph.PortDefinition{Type: graph.ValueType(p.Type), Required: p.Required}
	}
	if e := checkLoopBindings(op.Node, n, inputs, op.Bindings); e != nil {
		return e
	}
	// typed prediction
	loop := &graph.LoopDefinition{Gate: graph.LoopGateDefinition{Config: graph.NodeConfig(cfg)}, Bindings: []graph.LoopBinding{}}
	if op.MaxAttempts != nil {
		loop.MaxAttempts = *op.MaxAttempts
	}
	if len(inputs) > 0 {
		loop.Gate.Inputs = inputs
	}
	for _, b := range op.Bindings {
		loop.Bindings = append(loop.Bindings, graph.LoopBinding{From: b.From, To: b.To})
	}
	n.Loop = loop
	s.want.Nodes[op.Node] = n
	// tree
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return err
	}
	value := loopTree(op, inputs, cfgNode)
	if old, i := get(node, "loop"); i >= 0 {
		adoptValue(old, value)
		value.Style &^= yaml.FlowStyle
		node.Content[i+1] = value
		return nil
	}
	insertNodeKey(node, "loop", value)
	return nil
}

// checkLoopBindings refuses bindings the validator would refuse: a source that
// is not a declared candidate output or node input, a target that is not a
// declared gate input, unequal types, a repeated target, or no candidate
// output at all.
func checkLoopBindings(id string, n graph.NodeDefinition, gate map[string]graph.PortDefinition, bindings []LoopBinding) *Error {
	targets := map[string]bool{}
	candidate := false
	for _, b := range bindings {
		var source graph.PortDefinition
		var ok bool
		switch {
		case strings.HasPrefix(b.From, "candidate.outputs."):
			source, ok = n.Outputs[strings.TrimPrefix(b.From, "candidate.outputs.")]
			candidate = candidate || ok
		case strings.HasPrefix(b.From, "node.inputs."):
			source, ok = n.Inputs[strings.TrimPrefix(b.From, "node.inputs.")]
		}
		if !ok {
			return loopError(id, fmt.Sprintf("%q is not a declared candidate output or node input", b.From), "bind from candidate.outputs.PORT or node.inputs.PORT")
		}
		name := strings.TrimPrefix(b.To, "gate.inputs.")
		target, declared := gate[name]
		if !strings.HasPrefix(b.To, "gate.inputs.") || !declared {
			return loopError(id, fmt.Sprintf("%q is not a declared gate input", b.To), "bind to gate.inputs.PORT")
		}
		if target.Type != source.Type {
			return loopError(id, fmt.Sprintf("%s is %s but %s is %s", b.From, source.Type, b.To, target.Type), "bind ports of the same type")
		}
		if targets[name] {
			return loopError(id, fmt.Sprintf("gate input %q is bound twice", name), "bind each gate input once")
		}
		targets[name] = true
	}
	if !candidate {
		return loopError(id, "the loop checks no candidate output", "bind at least one candidate.outputs.PORT to the gate")
	}
	return nil
}

// loopTree builds the loop block: max_attempts when set, the gate's inputs in
// name order and its config in the client's key order, then the bindings.
func loopTree(op Op, inputs map[string]graph.PortDefinition, cfg *yaml.Node) *yaml.Node {
	gate := mapNode(false)
	if len(inputs) > 0 {
		names := make([]string, 0, len(inputs))
		for name := range inputs {
			names = append(names, name)
		}
		sort.Strings(names)
		ins := mapNode(false)
		for _, name := range names {
			p := mapNode(true, str("type"), str(string(inputs[name].Type)))
			if inputs[name].Required {
				p.Content = append(p.Content, str("required"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
			}
			ins.Content = append(ins.Content, str(name), p)
		}
		gate.Content = append(gate.Content, str("inputs"), ins)
	}
	gate.Content = append(gate.Content, str("config"), cfg)
	bindings := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, b := range op.Bindings {
		bindings.Content = append(bindings.Content, mapNode(true, str("from"), str(b.From), str("to"), str(b.To)))
	}
	if len(bindings.Content) == 0 {
		bindings.Style = yaml.FlowStyle
	}
	loop := mapNode(false)
	if op.MaxAttempts != nil {
		loop.Content = append(loop.Content, str("max_attempts"), intNode(*op.MaxAttempts))
	}
	loop.Content = append(loop.Content, str("gate"), gate, str("bindings"), bindings)
	return loop
}

// The targeted loop operations below change one part of an existing loop and
// patch the file in place: comments, key order and keys they do not name stay
// as written. set_loop stays for creating a loop.

// existingLoop returns node id with a private copy of its loop, and the loop
// mapping in the tree.
func (s *session) existingLoop(id string) (graph.NodeDefinition, *yaml.Node, *Error) {
	n, err := s.knownNode(id)
	if err != nil {
		return n, nil, err
	}
	if n.Loop == nil {
		return n, nil, loopError(id, fmt.Sprintf("node %q has no loop", id), "add the loop with set_loop first")
	}
	l := *n.Loop
	n.Loop = &l
	node, err := s.nodeTree(id)
	if err != nil {
		return n, nil, err
	}
	tree, _ := get(node, "loop")
	if tree == nil || tree.Kind != yaml.MappingNode {
		return n, nil, loopError(id, "the loop is not a mapping in the file", "fix the YAML text")
	}
	return n, tree, nil
}

func (s *session) setLoopAttempts(op Op) *Error {
	n, tree, err := s.existingLoop(op.Node)
	if err != nil {
		return err
	}
	if lo, hi := attemptBounds(); op.MaxAttempts != nil && (*op.MaxAttempts < lo || *op.MaxAttempts > hi) {
		e := loopError(op.Node, fmt.Sprintf("max_attempts must be from %d to %d", lo, hi), "")
		e.Path += ".max_attempts"
		return e
	}
	// typed prediction
	n.Loop.MaxAttempts = 0
	if op.MaxAttempts != nil {
		n.Loop.MaxAttempts = *op.MaxAttempts
	}
	s.want.Nodes[op.Node] = n
	// tree
	if op.MaxAttempts == nil {
		remove(tree, "max_attempts")
		return nil
	}
	if old, _ := get(tree, "max_attempts"); old != nil {
		old.Kind, old.Tag, old.Value, old.Style, old.Content = yaml.ScalarNode, "!!int", fmt.Sprint(*op.MaxAttempts), 0, nil
		return nil
	}
	tree.Content = append([]*yaml.Node{str("max_attempts"), intNode(*op.MaxAttempts)}, tree.Content...)
	return nil
}

// loopBindings converts typed bindings to the operation's spelling, for
// checkLoopBindings.
func loopBindings(in []graph.LoopBinding) []LoopBinding {
	out := make([]LoopBinding, 0, len(in))
	for _, b := range in {
		out = append(out, LoopBinding{From: b.From, To: b.To})
	}
	return out
}

func (s *session) setLoopGateInput(op Op) *Error {
	n, tree, err := s.existingLoop(op.Node)
	if err != nil {
		return err
	}
	if !graph.ValidIdentifier(op.Port) || !valueTypes[graph.ValueType(op.Type)] {
		return loopError(op.Node, fmt.Sprintf("gate input %q must have a valid name and type", op.Port), "")
	}
	port := graph.PortDefinition{Type: graph.ValueType(op.Type), Required: op.Required != nil && *op.Required}
	// typed prediction
	inputs := copyPorts(n.Loop.Gate.Inputs)
	if old, existed := inputs[op.Port]; existed && op.Required == nil {
		port.Required = old.Required
	}
	inputs[op.Port] = port
	if e := checkLoopBindings(op.Node, n, inputs, loopBindings(n.Loop.Bindings)); e != nil {
		return e
	}
	n.Loop.Gate.Inputs = inputs
	s.want.Nodes[op.Node] = n
	// tree
	gate, ok := childMapping(tree, "gate", true, func(v *yaml.Node) { tree.Content = append(tree.Content, str("gate"), v) })
	if !ok {
		return loopError(op.Node, "the loop gate is not a mapping in the file", "fix the YAML text")
	}
	m, ok := childMapping(gate, "inputs", true, func(v *yaml.Node) { gate.Content = append([]*yaml.Node{str("inputs"), v}, gate.Content...) })
	if !ok {
		return loopError(op.Node, "the gate inputs are not a mapping in the file", "fix the YAML text")
	}
	if value, _ := get(m, op.Port); value != nil {
		if value.Kind != yaml.MappingNode {
			return loopError(op.Node, fmt.Sprintf("gate input %q is not a mapping in the file", op.Port), "fix the YAML text")
		}
		setPortFields(value, port, true, op.Required != nil)
		return nil
	}
	value := mapNode(true, str("type"), str(op.Type))
	if port.Required {
		value.Content = append(value.Content, str("required"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	m.Content = append(m.Content, str(op.Port), value)
	return nil
}

func (s *session) removeLoopGateInput(op Op) *Error {
	n, tree, err := s.existingLoop(op.Node)
	if err != nil {
		return err
	}
	if _, ok := n.Loop.Gate.Inputs[op.Port]; !ok {
		e := loopError(op.Node, fmt.Sprintf("gate input %q does not exist", op.Port), "")
		e.Code = CodePortNotFound
		return e
	}
	// typed prediction
	inputs := copyPorts(n.Loop.Gate.Inputs)
	delete(inputs, op.Port)
	if e := checkLoopBindings(op.Node, n, inputs, loopBindings(n.Loop.Bindings)); e != nil {
		e.Suggestion = "remove the loop binding to this gate input first"
		return e
	}
	if len(inputs) == 0 {
		inputs = nil
	}
	n.Loop.Gate.Inputs = inputs
	s.want.Nodes[op.Node] = n
	// tree
	gate, _ := get(tree, "gate")
	m, _ := get(gate, "inputs")
	remove(m, op.Port)
	if m != nil && len(m.Content) == 0 {
		remove(gate, "inputs")
	}
	return nil
}

// setLoopBindings replaces the loop's bindings. A binding already in the file
// keeps its item (with its comments and style); new ones are added in the
// operation's order.
func (s *session) setLoopBindings(op Op) *Error {
	n, tree, err := s.existingLoop(op.Node)
	if err != nil {
		return err
	}
	if e := checkLoopBindings(op.Node, n, n.Loop.Gate.Inputs, op.Bindings); e != nil {
		return e
	}
	// typed prediction
	typed := make([]graph.LoopBinding, 0, len(op.Bindings))
	for _, b := range op.Bindings {
		typed = append(typed, graph.LoopBinding{From: b.From, To: b.To})
	}
	n.Loop.Bindings = typed
	s.want.Nodes[op.Node] = n
	// tree
	seq, i := get(tree, "bindings")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if i >= 0 {
			tree.Content[i+1] = seq
		} else {
			tree.Content = append(tree.Content, str("bindings"), seq)
		}
	}
	field := func(item *yaml.Node, key string) string {
		if v, _ := get(item, key); v != nil {
			return v.Value
		}
		return ""
	}
	used := make([]bool, len(seq.Content))
	items := make([]*yaml.Node, 0, len(op.Bindings))
	for _, b := range op.Bindings {
		var item *yaml.Node
		for j, old := range seq.Content {
			if !used[j] && field(old, "from") == b.From && field(old, "to") == b.To {
				used[j], item = true, old
				break
			}
		}
		if item == nil {
			item = mapNode(true, str("from"), str(b.From), str("to"), str(b.To))
		}
		items = append(items, item)
	}
	if len(seq.Content) == 0 && len(items) > 0 {
		seq.Style &^= yaml.FlowStyle
	}
	seq.Content = items
	return nil
}

func (s *session) clearLoop(op Op) *Error {
	n, err := s.knownNode(op.Node)
	if err != nil {
		return err
	}
	if n.Loop == nil {
		return nil
	}
	n.Loop = nil
	s.want.Nodes[op.Node] = n
	node, err := s.nodeTree(op.Node)
	if err != nil {
		return err
	}
	remove(node, "loop")
	return nil
}

func (s *session) setUnits(op Op) *Error {
	seen := map[string]bool{}
	for _, id := range op.Units {
		if _, ok := s.want.Nodes[id]; !ok {
			return &Error{Code: CodeUnitsInvalid, Message: fmt.Sprintf("node %q does not exist", id), OpIndex: -1, Node: id, Path: "units"}
		}
		if seen[id] {
			return &Error{Code: CodeUnitsInvalid, Message: fmt.Sprintf("node %q is listed twice", id), OpIndex: -1, Node: id, Path: "units"}
		}
		seen[id] = true
	}
	// typed prediction
	if len(op.Units) == 0 {
		s.want.Units = nil
		remove(s.top, "units")
		return nil
	}
	s.want.Units = append([]string(nil), op.Units...)
	// tree
	items := make([]*yaml.Node, 0, len(op.Units))
	for _, id := range op.Units {
		items = append(items, str(id))
	}
	if units, i := get(s.top, "units"); i >= 0 && units.Kind == yaml.SequenceNode {
		units.Content = items
		return nil
	}
	remove(s.top, "units")
	insertTop(s.top, "units", &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, Content: items})
	return nil
}
