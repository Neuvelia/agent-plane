package runtime

import (
	"testing"

	"agentplane/internal/domain/graph"
)

func TestIsolatedNodeRepointsGateWorkspaceAndCwdWithoutMutatingTheGraph(t *testing.T) {
	for _, key := range []string{"workspace", "cwd"} {
		node := graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"workspace": "/src"}, Loop: &graph.LoopDefinition{
			Gate: graph.LoopGateDefinition{Config: graph.NodeConfig{key: "/src", "kind": "x"}},
		}}
		got := isolatedNode(node, "/wt")
		if got.Config["workspace"] != "/wt" || got.Loop.Gate.Config[key] != "/wt" {
			t.Fatalf("%s: agent=%v gate=%v", key, got.Config, got.Loop.Gate.Config)
		}
		if node.Config["workspace"] != "/src" || node.Loop.Gate.Config[key] != "/src" {
			t.Fatalf("%s: the graph definition was mutated", key)
		}
	}
}
