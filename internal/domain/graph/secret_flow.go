package graph

import "fmt"

// validateSecretFlowToAgent rejects a data edge from a node that holds
// secret_env to an Agent node (F3.8, ADR-0029 addendum). A node's accepted
// output is immutable and flows raw to its dependents; a granted child can echo
// its secret, and an Agent's inputs are rendered into a prompt sent to a remote
// model. Only direct edges are checked: a path through an intermediate node
// without a secret is a documented gap (docs/features/F3.8-secrets-references.md).
//
// Edge and node references are echoed only after they parse as identifiers, so
// no free text from the graph reaches the message.
func (v *definitionValidator) validateSecretFlowToAgent() {
	for edgeIndex, edge := range v.definition.Edges {
		from, fromValid := parseNodeOutputReference(string(edge.From))
		to, toValid := parseNodeInputReference(string(edge.To))
		if !fromValid || !toValid {
			continue
		}
		source, sourceExists := v.definition.Nodes[from.nodeID]
		target, targetExists := v.definition.Nodes[to.nodeID]
		if !sourceExists || !targetExists || target.Type != "agent" || !holdsSecrets(source) {
			continue
		}
		v.error("E_SECRET_FLOWS_TO_AGENT",
			fmt.Sprintf("edge %s to %s carries the output of node %q, which is assigned secrets, into agent node %q",
				edge.From, edge.To, from.nodeID, to.nodeID),
			edgeLocation(edgeIndex, fmt.Sprintf("edges[%d]", edgeIndex)),
			fmt.Sprintf("remove the edge, pass the data through a node without a secret, or remove secret_env from node %q; an agent's inputs are sent to a remote model", from.nodeID))
	}
}

// holdsSecrets reports whether the node assigns at least one secret. An empty
// secret_env assigns nothing.
func holdsSecrets(node NodeDefinition) bool {
	return len(mapKeys(node.Config[secretEnvKey])) > 0
}
