package main

import (
	"fmt"
	"sort"
	"strings"

	"agentplane/internal/domain/graph"
)

// nodeTypeUnknownCode is the diagnostic for a node whose type no executor
// handles (#178). An empty type is graph.Validate's E_NODE_TYPE_REQUIRED.
const nodeTypeUnknownCode = "E_NODE_TYPE_UNKNOWN"

// nodeConfigCode is the diagnostic for a node whose static configuration the
// executors reject. Executors return plain errors, so one code carries them all.
const nodeConfigCode = "E_NODE_CONFIG_INVALID"

// validateGraph is the one validation path of the CLI, the API and the editor
// (ADR-0021): the graph rules, then each node's type against the node
// catalogue, then each shell, gate and agent node's static configuration,
// including a loop's embedded Gate. Human nodes are checked by graph.Validate.
// The executors' checks are side-effect free (nothing runs, nothing is read). Configuration diagnostics follow the graph ones in node-id
// order, so the result is deterministic.
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
	return result
}

// validateForRun is the gate before a run starts, for the CLI and `serve`: the
// graph rules and the node types. Node configuration is left to the executors,
// which report it as a run failure.
func validateForRun(def *graph.Definition) graph.ValidationResult {
	result := graph.Validate(def)
	result.Diagnostics = append(result.Diagnostics, nodeTypeDiagnostics(def, sortedNodeIDs(def))...)
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
