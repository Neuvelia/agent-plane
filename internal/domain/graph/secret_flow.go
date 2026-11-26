package graph

import (
	"fmt"
	"sort"
	"strings"
)

// validateSecretFlowToAgent rejects every Agent node reachable over data edges
// from a node that holds secret_env (F3.8, ADR-0029 addendum). A node's accepted
// output is immutable and flows raw to its dependents, and any node on the way
// may pass a value on, so no node type (shell, gate, human, agent) interrupts the
// propagation. An Agent's inputs are rendered into a prompt sent to a remote
// model. One diagnostic is reported per (source, agent) pair, with the shortest
// path (ties broken by edge order).
//
// Not covered, because they are not edges: a shared workspace (a Shell node
// writes a file an Agent reads; detection after the fact belongs to the
// workspace scan, F3.8 requirement 13) and the approval context shown by a
// human node. Node ids are echoed only after they parse as identifiers, so no
// free text from the graph reaches a message.
func (v *definitionValidator) validateSecretFlowToAgent() {
	steps := v.dataEdgeSteps()
	for _, source := range v.sortedNodeIDs() {
		if !holdsSecrets(v.definition.Nodes[source]) {
			continue
		}
		reach := shortestPaths(source, steps)
		agents := make([]string, 0, len(reach))
		for id := range reach {
			if v.definition.Nodes[id].Type == "agent" {
				agents = append(agents, id)
			}
		}
		sort.Strings(agents)
		for _, agent := range agents {
			v.reportSecretFlow(reach[agent])
		}
	}
}

// edgeStep is one valid data edge between two existing nodes.
type edgeStep struct {
	from, to  string
	edgeIndex int
}

// dataEdgeSteps indexes valid edges by source node, in edge order.
func (v *definitionValidator) dataEdgeSteps() map[string][]edgeStep {
	steps := map[string][]edgeStep{}
	for index, edge := range v.definition.Edges {
		from, fromValid := parseNodeOutputReference(string(edge.From))
		to, toValid := parseNodeInputReference(string(edge.To))
		if !fromValid || !toValid {
			continue
		}
		_, sourceExists := v.definition.Nodes[from.nodeID]
		_, targetExists := v.definition.Nodes[to.nodeID]
		if sourceExists && targetExists {
			steps[from.nodeID] = append(steps[from.nodeID], edgeStep{from: from.nodeID, to: to.nodeID, edgeIndex: index})
		}
	}
	return steps
}

// shortestPaths walks the edges breadth first from source and returns, for each
// reachable node other than source, the edge steps of its shortest path. Cycles
// are harmless: a node is expanded once.
func shortestPaths(source string, steps map[string][]edgeStep) map[string][]edgeStep {
	paths := map[string][]edgeStep{}
	visited := map[string]bool{source: true}
	queue := []string{source}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, step := range steps[current] {
			if visited[step.to] {
				continue
			}
			visited[step.to] = true
			paths[step.to] = append(append([]edgeStep(nil), paths[current]...), step)
			queue = append(queue, step.to)
		}
	}
	return paths
}

func (v *definitionValidator) reportSecretFlow(path []edgeStep) {
	first, last := path[0], path[len(path)-1]
	location := edgeLocation(first.edgeIndex, fmt.Sprintf("edges[%d]", first.edgeIndex))
	suggestion := fmt.Sprintf("remove the edge, pass the data through a node without a secret, or remove secret_env from node %q; an agent's inputs are sent to a remote model", first.from)
	if len(path) == 1 {
		edge := v.definition.Edges[first.edgeIndex]
		v.error("E_SECRET_FLOWS_TO_AGENT",
			fmt.Sprintf("edge %s to %s carries the output of node %q, which is assigned secrets, into agent node %q",
				edge.From, edge.To, first.from, last.to),
			location, suggestion)
		return
	}
	ids := []string{first.from}
	for _, step := range path {
		ids = append(ids, step.to)
	}
	edge := v.definition.Edges[first.edgeIndex]
	v.error("E_SECRET_FLOWS_TO_AGENT",
		fmt.Sprintf("the output of node %q, which is assigned secrets, can reach agent node %q through the path %s (first edge %s to %s)",
			first.from, last.to, strings.Join(ids, " -> "), edge.From, edge.To),
		location, suggestion)
}

// holdsSecrets reports whether the node assigns at least one secret. An empty
// secret_env assigns nothing.
func holdsSecrets(node NodeDefinition) bool {
	return len(mapKeys(node.Config[secretEnvKey])) > 0
}
