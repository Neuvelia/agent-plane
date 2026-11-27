package runtime

import (
	"errors"
	"os"
	"path/filepath"
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

// The source root is symlink-resolved, so a Gate directory authored through a
// symlink to the repository (/tmp on macOS) is inside it and maps into the
// worktree; a symlink that resolves elsewhere is still rejected.
func TestIsolatedGateDirectoryThroughASymlinkIsInsideTheRepository(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(root, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	outside := filepath.Join(base, "out")
	if err := os.Symlink(base, outside); err != nil {
		t.Fatal(err)
	}
	d := &graph.Definition{ID: "g", Nodes: map[string]graph.NodeDefinition{"a": isolatedAgent("cwd", filepath.Join(link, "svc"))}}
	if err := checkIsolatedGateDirs(d, []string{"a"}, root); err != nil {
		t.Fatalf("symlinked cwd rejected: %v", err)
	}
	if got := isolatedNode(d.Nodes["a"], root, "/wt").Loop.Gate.Config["cwd"]; got != filepath.Join("/wt", "svc") {
		t.Fatalf("cwd mapped to %v, want /wt/svc", got)
	}
	d.Nodes["a"] = isolatedAgent("cwd", outside)
	if err := checkIsolatedGateDirs(d, []string{"a"}, root); err == nil {
		t.Fatal("a symlink resolving outside the repository must be rejected")
	}
}
