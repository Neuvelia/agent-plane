package runtime

import (
	"fmt"
	"path/filepath"
	"strings"

	"agentplane/internal/domain/graph"
)

// isolatedGateKeys are the embedded Gate configuration keys that name a
// directory of the source repository.
func isolatedGateKeys() []string { return []string{"workspace", "cwd"} }

// isolatedNode is the node a worker executes: the same definition with the
// Agent pointed at the worktree and each Gate directory moved to the same
// place inside the worktree, so a cwd of <root>/service runs in
// <path>/service. preflightIsolation has checked that every such directory is
// inside root. The graph and its semantic version are never modified.
func isolatedNode(n graph.NodeDefinition, root, path string) graph.NodeDefinition {
	n.Config = cloneConfig(n.Config)
	n.Config["workspace"] = path
	if n.Loop != nil {
		loop := *n.Loop
		loop.Gate.Config = cloneConfig(loop.Gate.Config)
		for _, key := range isolatedGateKeys() {
			if dir, ok := loop.Gate.Config[key].(string); ok {
				if rel, inside := relativeToRoot(root, dir); inside {
					loop.Gate.Config[key] = filepath.Join(path, rel)
				}
			}
		}
		n.Loop = &loop
	}
	return n
}

// relativeToRoot returns dir relative to root when dir is root or below it.
// root is symlink-resolved (gitstate.CanonicalWorkspace), so an authored dir
// that reaches the repository through a symlink (/tmp -> /private/tmp on
// macOS) is compared resolved when its lexical form falls outside root.
func relativeToRoot(root, dir string) (string, bool) {
	if rel, inside := lexicallyBelow(root, dir); inside {
		return rel, true
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	return lexicallyBelow(root, resolved)
}

func lexicallyBelow(root, dir string) (string, bool) {
	rel, err := filepath.Rel(root, dir)
	if err != nil || !filepath.IsAbs(dir) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// checkIsolatedGateDirs rejects an embedded Gate directory outside the source
// repository: inside a worktree it would check the source checkout, not the
// isolated candidate.
func checkIsolatedGateDirs(d *graph.Definition, ids []string, root string) error {
	for _, id := range ids {
		for _, key := range isolatedGateKeys() {
			dir, ok := d.Nodes[id].Loop.Gate.Config[key].(string)
			if !ok {
				continue
			}
			if _, inside := relativeToRoot(root, dir); !inside {
				return isolationFailure(d, id, fmt.Sprintf("node %q: loop.gate.config.%s must be inside the source repository %s when isolation is worktree", id, key, root))
			}
		}
	}
	return nil
}

func cloneConfig(config graph.NodeConfig) graph.NodeConfig {
	copied := make(graph.NodeConfig, len(config)+1)
	for key, value := range config {
		copied[key] = cloneConfigValue(value)
	}
	return copied
}

func cloneConfigValue(value any) any {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		copied := make([]any, len(v))
		for i, item := range v {
			copied[i] = cloneConfigValue(item)
		}
		return copied
	case map[string]any:
		copied := make(map[string]any, len(v))
		for key, item := range v {
			copied[key] = cloneConfigValue(item)
		}
		return copied
	}
	return value
}
