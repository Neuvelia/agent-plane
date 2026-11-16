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
