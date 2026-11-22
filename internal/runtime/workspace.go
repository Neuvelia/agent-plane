package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/gitstate"
)

type workspaceIdentity struct {
	root     string
	worktree string
	gitDir   string
	known    bool
	isolated bool
}

// identityOutputLimit bounds the two path lines `git rev-parse` may print.
const identityOutputLimit = 16384

// Inspection is bounded, read-only, and does not inherit the caller's Git
// environment. Unknown identity always serializes, including non-Git roots.
func inspectWorkspace(ctx context.Context, workspace string) workspaceIdentity {
	if !filepath.IsAbs(workspace) || strings.ContainsAny(workspace, "\n\r\x00") {
		return workspaceIdentity{}
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return workspaceIdentity{}
	}
	root = filepath.Clean(root)
	result := workspaceIdentity{root: root}
	// gitstate.Run owns the Git process tree, so a cancelled run leaves no
	// Git child behind.
	res, err := gitstate.Run(ctx, root, gitstate.RunOptions{Timeout: 2 * time.Second, StdoutLimit: identityOutputLimit}, "rev-parse", "--show-toplevel", "--absolute-git-dir")
	if err != nil {
		return result
	}
	lines := strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n")
	if len(lines) != 2 {
		return result
	}
	top, e1 := filepath.EvalSymlinks(lines[0])
	gitDir, e2 := filepath.EvalSymlinks(lines[1])
	if e1 != nil || e2 != nil || !filepath.IsAbs(top) || !filepath.IsAbs(gitDir) || !nestedRoot(top, root) {
		return result
	}
	result.worktree = filepath.Clean(top)
	result.gitDir = filepath.Clean(gitDir)
	result.known = true
	return result
}
func inspectWorkspaces(ctx context.Context, d *graph.Definition) map[string]workspaceIdentity {
	out := map[string]workspaceIdentity{}
	cache := map[string]workspaceIdentity{}
	for _, id := range sortedDefinitionIDs(d) {
		if d.Nodes[id].Type == "agent" {
			root, _ := d.Nodes[id].Config["workspace"].(string)
			identity, ok := cache[root]
			if !ok {
				identity = inspectWorkspace(ctx, root)
				cache[root] = identity
			}
			out[id] = identity
		}
	}
	return out
}

// isolatedIdentity gives an isolated node a known, unique root that no real
// path can equal or contain (it holds a NUL), so scheduling never serializes it
// against another node.
func isolatedIdentity(nodeRunID events.NodeRunID) workspaceIdentity {
	root := "\x00isolated" + string(filepath.Separator) + string(nodeRunID)
	return workspaceIdentity{root: root, worktree: root, gitDir: root, known: true, isolated: true}
}
func nestedRoot(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, parent+string(filepath.Separator)) || parent == string(filepath.Separator)
}
func workspaceConflict(a, b workspaceIdentity) bool {
	if a.isolated || b.isolated {
		return false
	}
	return !a.known || !b.known || a.worktree == b.worktree || a.gitDir == b.gitDir || nestedRoot(a.root, b.root) || nestedRoot(b.root, a.root)
}
func declaredScope(n graph.NodeDefinition) ([]string, bool) {
	if _, ok := n.Config["allowed_paths"]; !ok {
		return nil, false
	}
	// A malformed unenrolled scope remains the executor's validation concern;
	// for diagnostics it cannot establish disjointness.
	switch a := n.Config["allowed_paths"].(type) {
	case []string:
	case []any:
		for _, v := range a {
			if _, ok := v.(string); !ok {
				return nil, false
			}
		}
	default:
		return nil, false
	}
	s, e := events.NormalizeUnitScope(originalScope(n))
	return s, e == nil
}
func declaredOverlap(a, b workspaceIdentity, as, bs []string) bool {
	if a.root == "" || b.root == "" {
		return false
	}
	for _, x := range as {
		for _, y := range bs {
			xp := filepath.Join(a.root, filepath.FromSlash(x))
			yp := filepath.Join(b.root, filepath.FromSlash(y))
			if xp == yp || strings.HasSuffix(x, "/") && nestedRoot(xp, yp) || strings.HasSuffix(y, "/") && nestedRoot(yp, xp) {
				return true
			}
		}
	}
	return false
}
func (m *runMachine) reportScopeConflicts(d *graph.Definition) {
	payload := events.DeclaredScopeConflictsPayload{Details: []events.DeclaredScopeConflict{}}
	add := func(a, b, reason string) {
		if len(payload.Details) < 64 {
			payload.Details = append(payload.Details, events.DeclaredScopeConflict{LeftNodeID: a, RightNodeID: b, Reason: reason})
		} else {
			payload.Omitted++
		}
	}
	for i, a := range m.ids {
		if d.Nodes[a].Type != "agent" || m.isolation.nodes[a] {
			continue
		}
		for _, b := range m.ids[i+1:] {
			if d.Nodes[b].Type != "agent" || m.isolation.nodes[b] {
				continue
			}
			ai, bi := m.workspaces[a], m.workspaces[b]
			as, ak := declaredScope(d.Nodes[a])
			bs, bk := declaredScope(d.Nodes[b])
			if !ai.known || !bi.known {
				add(a, b, "unknown_workspace_identity")
			}
			if workspaceConflict(ai, bi) {
				if !ak || !bk {
					add(a, b, "unknown_scope")
				} else if declaredOverlap(ai, bi, as, bs) {
					add(a, b, "declared_scope_overlap")
				}
			}
		}
	}
	if len(payload.Details) > 0 || payload.Omitted > 0 {
		m.appendEvent(events.Event{Type: events.DeclaredScopeConflicts, Payload: payload})
	}
}
