package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentplane/internal/events"
	"agentplane/internal/gitstate"
	"agentplane/internal/graph"
	"agentplane/internal/process"
)

type workspaceIdentity struct {
	root     string
	worktree string
	gitDir   string
	known    bool
}
type identityOutput struct{ data []byte }

func (b *identityOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 16384 {
		return 0, errors.New("workspace identity exceeds inspection bound")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

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
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	path := gitstate.Executable(bounded)
	if path == "" {
		return result
	}
	output := &identityOutput{}
	args := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "rev-parse", "--show-toplevel", "--absolute-git-dir"}
	var runErr error
	if supervisor := gitstate.Supervisor(bounded); supervisor != "" {
		outcome, err := process.Run(bounded, supervisor, process.Spec{Path: path, Args: args,
			Dir: root, Env: gitstate.InspectionEnvironment()}, output, io.Discard)
		runErr = err
		if runErr == nil && outcome.ExitCode != 0 {
			runErr = errors.New("Git workspace identity command exited unsuccessfully")
		}
	} else {
		cmd := exec.CommandContext(bounded, path, args...)
		cmd.Dir = root
		cmd.Env = gitstate.InspectionEnvironment()
		cmd.WaitDelay = time.Second
		cmd.Stdout = output
		runErr = cmd.Run()
	}
	if runErr != nil {
		return result
	}
	lines := strings.Split(strings.TrimSuffix(string(output.data), "\n"), "\n")
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
func nestedRoot(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, parent+string(filepath.Separator)) || parent == string(filepath.Separator)
}
func workspaceConflict(a, b workspaceIdentity) bool {
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
		if d.Nodes[a].Type != "agent" {
			continue
		}
		for _, b := range m.ids[i+1:] {
			if d.Nodes[b].Type != "agent" {
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
