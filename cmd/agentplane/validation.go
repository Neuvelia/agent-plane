package main

import (
	"fmt"
	"sort"
	"strings"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
)

// nodeTypeUnknownCode is the diagnostic for a node whose type no executor
// handles (#178). An empty type is graph.Validate's E_NODE_TYPE_REQUIRED.
const nodeTypeUnknownCode = "E_NODE_TYPE_UNKNOWN"

// nodeConfigCode is the diagnostic for a node whose static configuration the
// executors reject. Executors return plain errors, so one code carries them all.
const nodeConfigCode = "E_NODE_CONFIG_INVALID"

// edgeInputUnusedCode is the warning for an edge whose target node never reads
// the input the edge delivers (#180, F0.2 and F2.3).
const edgeInputUnusedCode = "W_EDGE_INPUT_UNUSED"

// humanNodeType is the node type of a local approval (ADR-0017).
const humanNodeType = "human"

// validateGraph is the one validation path of the CLI, the API and the editor
// (ADR-0021): the graph rules, then each node's type against the node
// catalogue, then each shell, gate and agent node's static configuration,
// including a loop's embedded Gate. Human nodes are checked by graph.Validate.
// The executors' checks are side-effect free (nothing runs, nothing is read). Configuration diagnostics follow the graph ones in node-id
// order, then the unused-edge-input warnings in edge order, so the result is
// deterministic.
func validateGraph(def *graph.Definition) graph.ValidationResult {
	result := graph.Validate(def)
	executor := nodeExecutor{}
	ids := sortedNodeIDs(def)
	result.Diagnostics = append(result.Diagnostics, nodeTypeDiagnostics(def, ids)...)
	for _, id := range ids {
		n := def.Nodes[id]
		if n.Type != "shell" && n.Type != "gate" && n.Type != "agent" {
			continue
		}
		report := func(err error, field string) {
			result.Diagnostics = append(result.Diagnostics, nodeConfigDiagnostic(def.ID, id, field, err))
		}
		if err := executor.ValidateNodeConfig(id, n); err != nil {
			report(err, "config")
		}
		if n.Loop != nil {
			if err := executor.ValidateNodeConfig(id, n.Loop.GateNode()); err != nil {
				report(err, "loop.gate")
			}
		}
	}
	result.Diagnostics = append(result.Diagnostics, unusedEdgeInputDiagnostics(def, result.Diagnostics)...)
	return result
}

// unusedEdgeInputDiagnostics warns, in edge order, about each edge whose
// target node never reads the input it delivers. A node reads an input that a
// field of its type's catalogue descriptors names (fields.NamedInputs) or that
// a loop binding forwards (node.inputs.X). An approval dependency always counts
// as used (isApprovalDependency). An edge with an unknown target, an
// undeclared input or an unknown node type, and an edge that an error in
// reported names, is left to the blocking diagnostics. Graph input bindings
// are not checked. The warning never blocks a run.
func unusedEdgeInputDiagnostics(def *graph.Definition, reported []graph.Diagnostic) []graph.Diagnostic {
	configFields := map[string][]fields.Field{}
	for _, t := range nodeCatalogue() {
		configFields[t.Type] = t.Config
	}
	invalid := invalidEdges(reported)
	var out []graph.Diagnostic
	for index, edge := range def.Edges {
		nodeID, inputID, ok := edgeTarget(edge)
		if !ok || invalid[index] {
			continue
		}
		n, exists := def.Nodes[nodeID]
		descriptors, known := configFields[n.Type]
		port, declared := n.Inputs[inputID]
		if !exists || !known || !declared {
			continue
		}
		if nodeReadsInput(n, configFields, inputID) || isApprovalDependency(def, edge, n) {
			continue
		}
		suggestion := unusedEdgeInputSuggestion(inputID, fields.InputKeys(descriptors, string(port.Type)), alwaysReadInputs(descriptors))
		out = append(out, unusedEdgeInputDiagnostic(def.ID, edgeInput{index: index, nodeID: nodeID, inputID: inputID}, suggestion))
	}
	return out
}

// invalidEdges returns the indexes of the edges a blocking diagnostic names.
func invalidEdges(diagnostics []graph.Diagnostic) map[int]bool {
	invalid := map[int]bool{}
	for _, d := range diagnostics {
		if d.Severity == graph.SeverityError && d.Location.EdgeIndex != nil {
			invalid[*d.Location.EdgeIndex] = true
		}
	}
	return invalid
}

// isApprovalDependency reports whether edge is part of a human approval
// (ADR-0017), where the dependency itself is the point and schema v0 has no
// ordering-only edge: an edge into a human node makes the approval wait for
// its source, and an edge from a human node's decision makes the target wait
// for the approval, whether or not either node reads the value.
func isApprovalDependency(def *graph.Definition, edge graph.EdgeDefinition, target graph.NodeDefinition) bool {
	if target.Type == humanNodeType {
		return true
	}
	parts := strings.Split(string(edge.From), ".")
	if len(parts) != 3 || parts[1] != "outputs" || parts[2] != "decision" {
		return false
	}
	source, exists := def.Nodes[parts[0]]
	return exists && source.Type == humanNodeType
}

// alwaysReadInputs returns, sorted, the inputs a node type reads with an
// empty configuration (an Agent's task).
func alwaysReadInputs(descriptors []fields.Field) []string {
	var names []string
	for name := range fields.NamedInputs(descriptors, map[string]any{}) {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// unusedEdgeInputSuggestion names the config keys that accept an input of
// this type, or else the inputs the node type always reads. It never advises
// removing the edge without saying that the edge is also the dependency.
func unusedEdgeInputSuggestion(inputID string, inputKeys, alwaysRead []string) string {
	const dependency = "; removing the edge would also remove the node's dependency on its source"
	if len(inputKeys) > 0 {
		return fmt.Sprintf("name input %q in config.%s so the node reads it%s", inputID, strings.Join(inputKeys, " or config."), dependency)
	}
	if len(alwaysRead) > 0 {
		return fmt.Sprintf("this node type reads only input %s; deliver the value there%s", strings.Join(quoteAll(alwaysRead), ", "), dependency)
	}
	return "this node type reads no input of this name or type" + dependency
}

func quoteAll(names []string) []string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return quoted
}

// edgeTarget splits an edge's NODE.inputs.PORT target. Text outside the
// identifier grammar is rejected: graph.Validate reports it.
func edgeTarget(edge graph.EdgeDefinition) (string, string, bool) {
	parts := strings.Split(string(edge.To), ".")
	if len(parts) != 3 || parts[1] != "inputs" || !graph.ValidIdentifier(parts[0]) || !graph.ValidIdentifier(parts[2]) {
		return "", "", false
	}
	return parts[0], parts[2], true
}

// nodeReadsInput reports whether n's configuration reads the named input, or
// whether its loop forwards it (from: node.inputs.<id>) to an embedded Gate
// input that the Gate's configuration reads. A forwarded input the Gate
// ignores is not read.
func nodeReadsInput(n graph.NodeDefinition, configFields map[string][]fields.Field, inputID string) bool {
	if fields.NamedInputs(configFields[n.Type], n.Config)[inputID] {
		return true
	}
	if n.Loop == nil {
		return false
	}
	gate := n.Loop.GateNode()
	gateReads := fields.NamedInputs(configFields[gate.Type], gate.Config)
	for _, binding := range n.Loop.Bindings {
		gateInput, forwarded := strings.CutPrefix(binding.To, "gate.inputs.")
		if binding.From == "node.inputs."+inputID && forwarded && gateReads[gateInput] {
			return true
		}
	}
	return false
}

// edgeInput identifies an edge by index and its target node and input, all
// in the identifier grammar.
type edgeInput struct {
	index   int
	nodeID  string
	inputID string
}

// unusedEdgeInputDiagnostic reports the edge and its target node and input.
func unusedEdgeInputDiagnostic(graphID string, edge edgeInput, suggestion string) graph.Diagnostic {
	index := edge.index
	return graph.Diagnostic{
		Severity:   graph.SeverityWarning,
		Code:       edgeInputUnusedCode,
		Message:    fmt.Sprintf("node %q does not use input %q, which edge %d delivers", edge.nodeID, edge.inputID, edge.index),
		GraphID:    shownGraphID(graphID),
		Location:   graph.DiagnosticLocation{EdgeIndex: &index, Path: fmt.Sprintf("edges[%d].to", edge.index)},
		Suggestion: suggestion,
	}
}

// validateForRun is the gate before a run starts, for the CLI and `serve`: the
// graph rules and the node types, then the non-blocking unused-edge-input
// warnings. Node configuration is left to the executors, which report it as a
// run failure.
func validateForRun(def *graph.Definition) graph.ValidationResult {
	result := graph.Validate(def)
	result.Diagnostics = append(result.Diagnostics, nodeTypeDiagnostics(def, sortedNodeIDs(def))...)
	result.Diagnostics = append(result.Diagnostics, unusedEdgeInputDiagnostics(def, result.Diagnostics)...)
	return result
}

// sortedNodeIDs returns the node ids in lexical order.
func sortedNodeIDs(def *graph.Definition) []string {
	ids := make([]string, 0, len(def.Nodes))
	for id := range def.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// nodeTypeDiagnostics reports, in the given node-id order, every node whose
// non-empty type is not in the node catalogue, the one list of node types the
// executors handle.
func nodeTypeDiagnostics(def *graph.Definition, ids []string) []graph.Diagnostic {
	var names []string
	known := map[string]bool{}
	for _, t := range nodeCatalogue() {
		names = append(names, t.Type)
		known[t.Type] = true
	}
	var out []graph.Diagnostic
	for _, id := range ids {
		if t := def.Nodes[id].Type; t != "" && !known[t] {
			out = append(out, unknownNodeTypeDiagnostic(def.ID, id, t, names))
		}
	}
	return out
}

// unknownNodeTypeDiagnostic reports a node type no executor handles. Like
// nodeConfigDiagnostic it echoes neither an id nor the type unless the text
// matches the identifier grammar: it may be a pasted secret.
func unknownNodeTypeDiagnostic(graphID, nodeID, nodeType string, knownTypes []string) graph.Diagnostic {
	shownType := graph.InvalidIdentifierText
	if graph.ValidIdentifier(nodeType) {
		shownType = fmt.Sprintf("%q", nodeType)
	}
	diagnostic := graph.Diagnostic{
		Severity:   graph.SeverityError,
		Code:       nodeTypeUnknownCode,
		Message:    fmt.Sprintf("node type %s is not supported", shownType),
		GraphID:    shownGraphID(graphID),
		Location:   graph.DiagnosticLocation{Path: "nodes"},
		Suggestion: "use one of the node types: " + strings.Join(knownTypes, ", "),
	}
	if graph.ValidIdentifier(nodeID) {
		diagnostic.Message = fmt.Sprintf("node %q has unsupported type %s", nodeID, shownType)
		diagnostic.Location = graph.DiagnosticLocation{NodeID: nodeID, Path: "nodes." + nodeID + ".type"}
	}
	return diagnostic
}

// nodeConfigDiagnostic reports an executor's rejection of one node. Like the
// graph validator, it never echoes a graph or node id outside the identifier
// grammar: such text may be a pasted secret, so the location is then the
// nodes collection and the ids are left out.
func nodeConfigDiagnostic(graphID, nodeID, field string, err error) graph.Diagnostic {
	diagnostic := graph.Diagnostic{
		Severity:   graph.SeverityError,
		Code:       nodeConfigCode,
		Message:    err.Error(),
		Location:   graph.DiagnosticLocation{Path: "nodes"},
		Suggestion: "correct the configuration of this node",
	}
	diagnostic.GraphID = shownGraphID(graphID)
	if graph.ValidIdentifier(nodeID) {
		diagnostic.Location = graph.DiagnosticLocation{NodeID: nodeID, Path: "nodes." + nodeID + "." + field}
		diagnostic.Suggestion = fmt.Sprintf("correct the configuration of node %q", nodeID)
	}
	return diagnostic
}

// diagnosticsFor is validateGraph's list, for the API.
func diagnosticsFor(def *graph.Definition) []graph.Diagnostic { return validateGraph(def).Diagnostics }

// shownGraphID is the graph id as output may carry it: empty when the id fails
// the identifier grammar (E_GRAPH_ID_INVALID reports it without the text).
func shownGraphID(id string) string {
	if graph.ValidIdentifier(id) {
		return id
	}
	return ""
}

// graphIDText is shownGraphID for prose.
func graphIDText(id string) string {
	if graph.ValidIdentifier(id) {
		return id
	}
	return graph.InvalidIdentifierText
}
