package graph

import (
	"fmt"
	"strings"
)

// ValidateDataflow runs only the passes of Validate that read edges, graph
// input bindings or the input providers they create: edge references and types,
// secret flow to agents, graph input bindings, required inputs and cycles. It
// runs them in Validate's order and reports their diagnostics exactly as
// Validate does.
//
// The other passes read only nodes, ports, inputs, outputs, units and secrets,
// and use other diagnostic codes. So for two definitions that differ only in
// Edges and GraphInputBindings, the blocking diagnostics Validate reports for
// one and not the other are the ones ValidateDataflow reports for one and not
// the other, in the same order. Editors that judge many candidate connections
// against one definition use it to skip the passes a connection cannot change
// (issue #192); TestValidateDataflowMatchesValidateForConnectionEdits pins it.
func ValidateDataflow(definition *Definition) ValidationResult {
	// usedOutputs stays nil: it feeds only the unused-output warnings, which this
	// subset does not run.
	validator := definitionValidator{definition: definition}
	if definition == nil {
		validator.error("E_DEFINITION_NIL", "graph definition is nil", DiagnosticLocation{}, "provide a parsed graph definition")
		return validator.result()
	}
	validator.validateEdges()
	validator.validateSecretFlowToAgent()
	validator.validateGraphInputBindings()
	validator.validateRequiredInputs()
	validator.validateCycles()
	return validator.result()
}

// sortedNodes returns the sorted node IDs, computed once per validation: several
// passes walk the nodes in this order and Nodes does not change while one runs.
func (v *definitionValidator) sortedNodes() []string {
	if v.nodeIDs == nil {
		v.nodeIDs = sortedNodeIDs(v.definition.Nodes)
	}
	return v.nodeIDs
}

// portKey identifies one port of one node in the validator's maps.
type portKey struct {
	nodeID string
	portID string
}

// resolvedEdge is an edge whose references parse and whose ports exist.
type resolvedEdge struct {
	from     nodeOutputReference
	to       nodeInputReference
	fromPort PortDefinition
	toPort   PortDefinition
}

// validateSoundEdge handles an edge whose references parse and whose ports
// exist, without building the paths that only a reference error would need.
// It returns false for any other edge, which validateEdges reports in full.
// Both paths end in acceptEdge, so the diagnostics are the same.
func (v *definitionValidator) validateSoundEdge(edgeIndex int, edge EdgeDefinition) bool {
	from, ok := parseNodeOutputReference(string(edge.From))
	if !ok {
		return false
	}
	to, ok := parseNodeInputReference(string(edge.To))
	if !ok {
		return false
	}
	fromPort, ok := v.definition.Nodes[from.nodeID].Outputs[from.portID]
	if !ok {
		return false
	}
	toPort, ok := v.definition.Nodes[to.nodeID].Inputs[to.portID]
	if !ok {
		return false
	}
	v.acceptEdge(edgeIndex, edge, resolvedEdge{from: from, to: to, fromPort: fromPort, toPort: toPort})
	return true
}

// acceptEdge records a resolved edge: its dependency, a type mismatch, or its
// provider and the output it uses.
func (v *definitionValidator) acceptEdge(edgeIndex int, edge EdgeDefinition, resolved resolvedEdge) {
	v.dependencies = append(v.dependencies, dependency{from: resolved.from.nodeID, to: resolved.to.nodeID, edgeIndex: edgeIndex})
	fromType, toType := resolved.fromPort.Type, resolved.toPort.Type
	if !supportedValueType(fromType) || !supportedValueType(toType) {
		return
	}
	if fromType != toType {
		v.error("E_BINDING_TYPE_MISMATCH", fmt.Sprintf("edge binds %q (%s) to %q (%s)", edge.From, fromType, edge.To, toType), edgeLocation(edgeIndex, fmt.Sprintf("edges[%d]", edgeIndex)), "bind ports with the same declared type")
		return
	}
	if v.countProvider(resolved.to.nodeID, resolved.to.portID) {
		v.reportMultipleProviders(resolved.to.nodeID, resolved.to.portID, edgeLocation(edgeIndex, ""))
	}
	if v.usedOutputs != nil {
		v.usedOutputs[outputKey(resolved.from.nodeID, resolved.from.portID)] = true
	}
}

// splitReference splits a reference with exactly two dots into its three
// parts, as strings.Split(reference, ".") with three results would.
func splitReference(reference string) (string, string, string, bool) {
	first, rest, ok := strings.Cut(reference, ".")
	if !ok {
		return "", "", "", false
	}
	middle, last, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(last, ".") {
		return "", "", "", false
	}
	return first, middle, last, true
}
