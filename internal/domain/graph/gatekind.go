package graph

// gateKindTestCommand is the Gate kind that runs an author-chosen test command
// (ADR-0042). It writes, so it may run only as a loop's embedded Gate, where a
// rejected attempt already leaves side effects (ADR-0018).
const gateKindTestCommand = "test_command"

// nodeTypeGate is the Gate node type; the graph package names it where a rule
// depends on it.
const nodeTypeGate = "gate"

// validateLoopOnlyGateKinds rejects a standalone Gate node whose kind may run
// only inside a loop (#183, ADR-0042). The Gate executor refuses the same call
// at run time, so the rule holds for callers that skip validation.
func (v *definitionValidator) validateLoopOnlyGateKinds() {
	for _, id := range v.sortedNodes() {
		n := v.definition.Nodes[id]
		if n.Type != nodeTypeGate || n.Config["kind"] != gateKindTestCommand {
			continue
		}
		v.error("E_GATE_KIND_LOOP_ONLY", "a test_command Gate is valid only as a loop's embedded Gate",
			DiagnosticLocation{NodeID: id, Path: "nodes." + id + ".config.kind"},
			"move the check into loop.gate of the node that produces the change")
	}
}
