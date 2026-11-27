package graph

import (
	"fmt"
	"strings"
)

// InputUse reports whether the static configuration of node reads its declared
// input inputID. The graph package does not know the configuration keys of the
// Shell, Gate and Agent executors; the composition root derives an InputUse
// from their field descriptors (internal/fields) and passes it to ValidateWith.
// A loop that forwards node.inputs.<id> to an embedded Gate input is resolved
// here by asking the InputUse about that Gate, so an InputUse needs to judge
// only the configuration of the node it is given.
type InputUse func(node NodeDefinition, inputID string) bool

// ValidateWith is Validate plus the checks that need to know which inputs a
// node reads: the non-blocking W_EDGE_INPUT_UNUSED warning (F0.2, #180) and
// the stricter routing rule of human on_reject: continue (ADR-0043). A nil use
// is Validate.
func ValidateWith(definition *Definition, use InputUse) ValidationResult {
	return validate(definition, use)
}

// reads reports whether node reads inputID: through its own configuration, or
// through a loop binding that forwards it to an embedded Gate input the Gate's
// configuration reads. A forwarded input the Gate ignores is not read. Without
// an InputUse, every input counts as read.
func (v *definitionValidator) reads(node NodeDefinition, inputID string) bool {
	if v.inputUse == nil {
		return true
	}
	return v.inputUse(node, inputID) || v.loopGateReads(node, inputID)
}

// loopGateReads reports whether node's loop forwards node.inputs.<inputID> to
// a Gate input that the embedded Gate reads.
func (v *definitionValidator) loopGateReads(node NodeDefinition, inputID string) bool {
	if node.Loop == nil {
		return false
	}
	gate := node.Loop.GateNode()
	for _, binding := range node.Loop.Bindings {
		gateInput, forwarded := strings.CutPrefix(binding.To, "gate.inputs.")
		if binding.From == "node.inputs."+inputID && forwarded && v.inputUse(gate, gateInput) {
			return true
		}
	}
	return false
}

// validateUnconsumedInputs warns when a valid edge delivers a value to a node
// input that the target's configuration never reads. Two edges are exempt
// because the edge itself is the contract (F2.10, ADR-0017): an edge from a
// human node's decision (it is what makes the target wait for approval) and an
// edge into a human node (the approval panel shows every input, ADR-0043).
func (v *definitionValidator) validateUnconsumedInputs() {
	if v.inputUse == nil {
		return
	}
	reported := make(map[portKey]bool)
	for _, dep := range v.dependencies {
		edge := v.definition.Edges[dep.edgeIndex]
		to, ok := parseNodeInputReference(string(edge.To))
		if !ok || v.approvalEdge(edge, to) {
			continue
		}
		node := v.definition.Nodes[to.nodeID]
		key := inputKey(to.nodeID, to.portID)
		if reported[key] || v.reads(node, to.portID) {
			continue
		}
		reported[key] = true
		v.warning("W_EDGE_INPUT_UNUSED",
			fmt.Sprintf("input %q on node %q receives edge %q but the %s node does not read it", to.portID, to.nodeID, edge.From, node.Type),
			edgeLocation(dep.edgeIndex, fmt.Sprintf("edges[%d].to", dep.edgeIndex)),
			unreadInputSuggestion(node.Type, to.portID))
	}
}

// approvalEdge reports whether edge carries a human decision or feeds a human
// node: such an edge is exempt from W_EDGE_INPUT_UNUSED.
func (v *definitionValidator) approvalEdge(edge EdgeDefinition, to nodeInputReference) bool {
	if v.definition.Nodes[to.nodeID].Type == "human" {
		return true
	}
	from, ok := parseNodeOutputReference(string(edge.From))
	return ok && from.portID == "decision" && v.definition.Nodes[from.nodeID].Type == "human"
}

// unreadInputSuggestion names where the target type reads an input.
func unreadInputSuggestion(nodeType, inputID string) string {
	switch nodeType {
	case "shell":
		return fmt.Sprintf("read the input through config.input_env, for example input_env: {%s: NAME}, or remove the edge", inputID)
	case nodeTypeGate:
		return "name the input in the Gate configuration, or remove the edge"
	case "agent":
		return "an Agent reads only its task input; remove the edge or bind the value to task"
	}
	return "use the input in the node configuration, or remove the edge"
}
