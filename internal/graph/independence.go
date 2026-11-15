package graph

import "sort"

// IndependentNodes returns, for every node, the sorted ids of the nodes that
// have no data-dependency path to or from it (F2.2 requirement 13). Paths
// follow the edges only: schema v0 has no ordering edges, graph input bindings
// connect a node to the graph rather than to another node, and a loop's
// bindings stay inside its node. Edges whose references do not parse are
// ignored; they are reported by Validate. The result is deterministic and
// defined for invalid graphs too (a cycle simply makes its members dependent).
func IndependentNodes(def *Definition) map[string][]string {
	out := map[string][]string{}
	if def == nil {
		return out
	}
	next := map[string][]string{}
	for _, e := range def.Edges {
		from, okFrom := parseNodeOutputReference(string(e.From))
		to, okTo := parseNodeInputReference(string(e.To))
		if !okFrom || !okTo {
			continue
		}
		next[from.nodeID] = append(next[from.nodeID], to.nodeID)
	}
	ids := make([]string, 0, len(def.Nodes))
	for id := range def.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	reach := make(map[string]map[string]bool, len(ids))
	for _, id := range ids {
		seen := map[string]bool{}
		stack := append([]string(nil), next[id]...)
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[n] {
				continue
			}
			seen[n] = true
			stack = append(stack, next[n]...)
		}
		reach[id] = seen
	}
	for _, id := range ids {
		list := []string{}
		for _, other := range ids {
			if other != id && !reach[id][other] && !reach[other][id] {
				list = append(list, other)
			}
		}
		out[id] = list
	}
	return out
}
