package runtime

import (
	"errors"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

func isolatedAgent(gateKey, gateDir string) graph.NodeDefinition {
	return graph.NodeDefinition{Type: "agent", Config: graph.NodeConfig{"workspace": "/src"}, Loop: &graph.LoopDefinition{
		Gate: graph.LoopGateDefinition{Config: graph.NodeConfig{gateKey: gateDir, "kind": "x"}},
	}}
}

// The embedded Gate's directory keeps its place in the repository: a cwd of
// <root>/service runs in <worktree>/service, not at the worktree root (review
// of #198).
func TestIsolatedNodeMovesGateDirectoriesToTheSamePlaceInTheWorktree(t *testing.T) {
	for _, tc := range []struct{ key, dir, want string }{
		{"workspace", "/src", "/wt"},
		{"cwd", "/src", "/wt"},
		{"cwd", "/src/service", "/wt/service"},
		{"cwd", "/src/a/b", "/wt/a/b"},
	} {
		node := isolatedAgent(tc.key, tc.dir)
		got := isolatedNode(node, "/src", "/wt")
		if got.Config["workspace"] != "/wt" || got.Loop.Gate.Config[tc.key] != tc.want {
			t.Fatalf("%s=%s: agent=%v gate=%v, want gate %s", tc.key, tc.dir, got.Config, got.Loop.Gate.Config, tc.want)
		}
		if node.Config["workspace"] != "/src" || node.Loop.Gate.Config[tc.key] != tc.dir {
			t.Fatalf("%s: the graph definition was mutated", tc.key)
		}
	}
}

// A Gate directory outside the source repository would check the source
// checkout instead of the isolated candidate, so preflight rejects it.
func TestIsolatedGateDirectoryOutsideTheSourceRepositoryIsRejected(t *testing.T) {
	for _, tc := range []struct {
		dir string
		ok  bool
	}{{"/src", true}, {"/src/service", true}, {"/elsewhere", false}, {"/src2", false}, {"/", false}, {"relative", false}} {
		d := &graph.Definition{ID: "g", Nodes: map[string]graph.NodeDefinition{"a": isolatedAgent("cwd", tc.dir)}}
		err := checkIsolatedGateDirs(d, []string{"a"}, "/src")
		var preflight *run.PreflightError
		if tc.ok != (err == nil) || (err != nil && !errors.As(err, &preflight)) {
			t.Fatalf("cwd %s: err=%v, want ok=%v", tc.dir, err, tc.ok)
		}
	}
}
