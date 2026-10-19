package graph

import "strings"

func (v *definitionValidator) validateLoops() {
	for _, id := range sortedNodeIDs(v.definition.Nodes) {
		n := v.definition.Nodes[id]
		l := n.Loop
		if l == nil {
			continue
		}
		fail := func(message string) {
			v.error("E_LOOP_INVALID", message, DiagnosticLocation{NodeID: id, Path: "nodes." + id + ".loop"}, "declare an Agent loop with a bounded budget and explicit typed Gate bindings")
		}
		if n.Type != "agent" {
			fail("loop producer must be an Agent")
		}
		if l.Attempts() < 1 || l.Attempts() > 10 {
			fail("max_attempts must be from 1 to 10")
		}
		if _, ok := n.Config["allowed_paths"]; !ok {
			fail("loop Agent requires explicit allowed_paths")
		}
		if len(l.Gate.Config) == 0 {
			fail("loop requires an embedded Gate")
		}
		targets := map[string]bool{}
		candidate := false
		for _, b := range l.Bindings {
			var source PortDefinition
			var ok bool
			p := strings.Split(b.From, ".")
			if len(p) == 3 && p[0] == "candidate" && p[1] == "outputs" {
				source, ok = n.Outputs[p[2]]
				candidate = true
			} else if len(p) == 3 && p[0] == "node" && p[1] == "inputs" {
				source, ok = n.Inputs[p[2]]
			}
			q := strings.Split(b.To, ".")
			if !ok || len(q) != 3 || q[0] != "gate" || q[1] != "inputs" {
				fail("invalid loop source or target binding")
				continue
			}
			target, exists := l.Gate.Inputs[q[2]]
			if !exists || target.Type != source.Type || targets[q[2]] {
				fail("loop bindings require declared matching types and unique targets")
			}
			targets[q[2]] = true
		}
		if !candidate {
			fail("loop requires a candidate output dependency")
		}
		for _, name := range sortedPortIDs(l.Gate.Inputs) {
			port := l.Gate.Inputs[name]
			v.validateIdentifier(name, "nodes."+id+".loop.gate.inputs."+name, "Gate input", id)
			v.validateValueType(port.Type, "nodes."+id+".loop.gate.inputs."+name, id)
			if port.Required && !targets[name] {
				fail("required Gate input has no binding")
			}
		}
	}
}
