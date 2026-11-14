package main

import (
	"fmt"
	"sort"

	"agentplane/internal/graph"
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
		report := func(err error, path string) {
			result.Diagnostics = append(result.Diagnostics, graph.Diagnostic{
				Severity:   graph.SeverityError,
				Code:       nodeConfigCode,
				Message:    err.Error(),
				GraphID:    def.ID,
				Location:   graph.DiagnosticLocation{NodeID: id, Path: path},
				Suggestion: fmt.Sprintf("correct the configuration of node %q", id),
			})
		}
		if err := executor.ValidateNodeConfig(id, n); err != nil {
			report(err, "nodes."+id+".config")
		}
		if n.Loop != nil {
			if err := executor.ValidateNodeConfig(id, n.Loop.GateNode()); err != nil {
				report(err, "nodes."+id+".loop.gate")
			}
		}
	}
	return result
}

// diagnosticsFor is validateGraph's list, for the API.
func diagnosticsFor(def *graph.Definition) []graph.Diagnostic { return validateGraph(def).Diagnostics }
