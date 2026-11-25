package main

import (
	"fmt"
	"sort"

	"agentplane/internal/domain/graph"
)

// nodeConfigCode is the diagnostic for a node whose static configuration the
// executors reject. Executors return plain errors, so one code carries them all.
const nodeConfigCode = "E_NODE_CONFIG_INVALID"

// validateGraph is the one validation path of the CLI, the API and the editor
// (ADR-0021): the graph rules, then each shell, gate and agent node's static
// configuration, including a loop's embedded Gate. Human nodes are checked by
// graph.Validate. The executors' checks are side-effect free (nothing runs,
// nothing is read). Configuration diagnostics follow the graph ones in node-id
// order, so the result is deterministic.
func validateGraph(def *graph.Definition) graph.ValidationResult {
	result := graph.Validate(def)
	executor := nodeExecutor{}
	ids := make([]string, 0, len(def.Nodes))
	for id := range def.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
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
	return "<invalid identifier>"
}
