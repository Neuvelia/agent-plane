package main

import (
	"agentplane/internal/agent"
	"agentplane/internal/api"
	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
	"agentplane/internal/gate"
	"agentplane/internal/shell"
)

// standaloneGateKinds are the kinds a Gate node offers on its own (F2.2
// requirement 2). The Gate executor also accepts changed_files_scope, which
// belongs to an Agent loop's embedded Gate; the inspector does not offer it
// on a standalone Gate (F2.2 Open questions).
var standaloneGateKinds = []string{gate.KindCommand, gate.KindJSONPredicate, gate.KindReportedFileScope}

// nodeCatalogue composes the inspector's node types from the descriptors each
// validator owns. Only which Gate kinds a node type offers is decided here.
func nodeCatalogue() []api.NodeType {
	return []api.NodeType{
		{Type: "agent", Config: agent.ConfigFields(), Outputs: agent.OutputPorts(), Loop: loopType("agent")},
		{Type: "gate", Config: gateFields(standaloneGateKinds), Outputs: gate.OutputPorts(), Loop: loopType("gate")},
		{Type: "human", Config: graph.HumanConfigFields(), Outputs: graph.HumanOutputPorts(), OutputsLocked: true, Loop: loopType("human")},
		{Type: "shell", Config: shell.ConfigFields(), Outputs: shell.OutputPorts(), Loop: loopType("shell")},
	}
}

func loopType(nodeType string) *api.LoopType {
	if !graph.LoopProducer(nodeType) {
		return nil
	}
	kinds := graph.LoopGateKinds(nodeType)
	return &api.LoopType{Fields: graph.LoopFields(), Gate: gateFields(kinds), GateKinds: kinds}
}

// gateFields narrows the Gate descriptors to kinds: the kind enum lists only
// those kinds, in the executor's order, and the groups of other kinds are
// dropped. Every other descriptor is the executor's, unchanged.
func gateFields(kinds []string) []fields.Field {
	offered := map[string]bool{}
	for _, k := range kinds {
		offered[k] = true
	}
	var out []fields.Field
	for _, f := range gate.ConfigFields() {
		switch {
		case f.Key == "kind":
			var enum []string
			for _, k := range f.Enum {
				if offered[k] {
					enum = append(enum, k)
				}
			}
			f.Enum = enum
		case f.Kind == fields.Group && f.Key == "" && len(f.Requires) == 1 && f.Requires[0].Key == "kind":
			if !offered[f.Requires[0].Equals] {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}
