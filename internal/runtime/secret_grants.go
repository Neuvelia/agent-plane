package runtime

import (
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

// secretEnvFor returns the secret environment an invocation may receive. Only
// the node the graph assigned the secret to, running as its own type, gets it:
// the embedded Gate of a loop is invoked with its producer's node ID but with
// node type "gate", so it is cut off here and never sees a value. The grant is
// read from the run's in-memory map at the one point a node process starts and
// is passed by value for each attempt (F3.8, ADR-0029).
func (m *runMachine) secretEnvFor(id string, node graph.NodeDefinition) secrets.Env {
	assigned, ok := m.definition.Nodes[id]
	if !ok || assigned.Type != node.Type {
		return secrets.Env{}
	}
	return m.grants[id]
}
