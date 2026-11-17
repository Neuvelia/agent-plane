package runtime

import (
	"context"
	"fmt"
	"sort"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
)

const nodeTypeAgent = "agent"

// RetainPolicy selects which isolated worktrees outlive their node run
// (ADR-0024). Run-scoped refs are always kept; they are the deliverable.
type RetainPolicy string

const (
	// RetainNever removes every isolated worktree when its node run ends.
	RetainNever RetainPolicy = "never"
	// RetainFailed removes accepted-and-integrated worktrees and keeps the
	// rest. It is the default.
	RetainFailed RetainPolicy = "failed"
	// RetainAlways removes nothing.
	RetainAlways RetainPolicy = "always"
)

// WorkspaceSource is the repository an isolated run starts from; BaseSHA is
// resolved once by WorkspaceManager.Preflight.
type WorkspaceSource struct {
	Root    string
	BaseSHA string
}

// Workspace is one isolated working directory created for a node run.
type Workspace interface{ Path() string }

// WorkspaceSnapshot is the commit recorded for an accepted unit.
type WorkspaceSnapshot struct {
	Commit       string
	ChangedPaths int
}

// WorkspaceIntegration is the outcome of merging one unit onto the run's tip.
// On conflict nothing was written and TipCommit is the unchanged tip.
type WorkspaceIntegration struct {
	Conflict      bool
	ConflictPaths []string
	Omitted       int
	TipCommit     string
	ChangedPaths  int
}

// WorkspaceManager is the consumer-owned port to Git worktree isolation. The
// runtime owns lifecycle and acceptance state; implementations own Git and
// must never write the user's branch, index or working tree. Every error text
// must be safe to show: it is never copied into events.
type WorkspaceManager interface {
	Preflight(ctx context.Context, sourceWorkspace string) (WorkspaceSource, error)
	Create(ctx context.Context, source WorkspaceSource, runID, nodeRunID string) (Workspace, error)
	Snapshot(ctx context.Context, workspace Workspace, unitRef string) (WorkspaceSnapshot, error)
	Integrate(ctx context.Context, source WorkspaceSource, tipRef, unitCommit string) (WorkspaceIntegration, error)
	Remove(ctx context.Context, workspace Workspace) error
	UnitRef(runID, nodeID string) (string, error)
	IntegratedRef(runID string) (string, error)
}

// isolationPlan is what preflight established about isolation for one run.
type isolationPlan struct {
	manager WorkspaceManager
	policy  RetainPolicy
	source  WorkspaceSource
	nodes   map[string]bool
}

func (p isolationPlan) active() bool { return len(p.nodes) > 0 }

// isolationRequested reports whether a node asks for a worktree and rejects an
// unknown value. An absent value and "none" are the shared-checkout default.
func isolationRequested(n graph.NodeDefinition) (bool, error) {
	raw, present := n.Config["isolation"]
	if !present {
		return false, nil
	}
	switch raw {
	case "none":
		return false, nil
	case "worktree":
		return true, nil
	}
	return false, fmt.Errorf(`isolation must be "none" or "worktree", got %q`, fmt.Sprint(raw))
}

func isolationFailure(d *graph.Definition, id, message string) error {
	return &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{
		Severity: graph.SeverityError, Code: "E_ISOLATION_PREFLIGHT", GraphID: d.ID,
		Location:   graph.DiagnosticLocation{NodeID: id, Path: "nodes." + id + ".config.isolation"},
		Message:    message,
		Suggestion: "set isolation only on a loop Agent listed in units, in a clean Git repository, with a workspace manager configured",
	}}}}
}

// preflightIsolation validates every node that opts in and resolves the single
// source repository, before run_started. Nothing is created or emitted here.
func preflightIsolation(ctx context.Context, d *graph.Definition, options Options) (isolationPlan, error) {
	policy := options.RetainWorktrees
	switch policy {
	case "":
		policy = RetainFailed
	case RetainNever, RetainFailed, RetainAlways:
	default:
		return isolationPlan{}, &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{
			Severity: graph.SeverityError, Code: "E_ISOLATION_RETAIN_POLICY", GraphID: d.ID,
			Message:    fmt.Sprintf("invalid retain-worktrees policy %q", string(policy)),
			Suggestion: "use never, failed or always",
		}}}}
	}
	nodes, err := isolatedNodes(d)
	if err != nil {
		return isolationPlan{}, err
	}
	plan := isolationPlan{manager: options.Workspaces, policy: policy, nodes: nodes}
	if !plan.active() {
		return plan, nil
	}
	ids := sortedKeys(nodes)
	if options.Workspaces == nil {
		return isolationPlan{}, isolationFailure(d, ids[0], fmt.Sprintf("node %q: isolation worktree needs a workspace manager; none is configured for this run", ids[0]))
	}
	plan.source, err = resolveIsolationSource(ctx, d, ids, options.Workspaces)
	return plan, err
}

// isolatedNodes returns the nodes that ask for a worktree and rejects any
// request on a node that cannot honor it.
func isolatedNodes(d *graph.Definition) (map[string]bool, error) {
	enrolled := make(map[string]bool, len(d.Units))
	for _, id := range d.Units {
		enrolled[id] = true
	}
	nodes := map[string]bool{}
	for _, id := range sortedDefinitionIDs(d) {
		n := d.Nodes[id]
		requested, err := isolationRequested(n)
		if err != nil {
			return nil, isolationFailure(d, id, fmt.Sprintf("node %q: %v", id, err))
		}
		if !requested {
			continue
		}
		if n.Type != nodeTypeAgent || n.Loop == nil || !enrolled[id] {
			return nil, isolationFailure(d, id, fmt.Sprintf("node %q: isolation worktree requires a loop-enabled Agent node listed in units", id))
		}
		nodes[id] = true
	}
	return nodes, nil
}

// resolveIsolationSource calls the manager once per distinct workspace string
// and requires every isolated node to resolve to one repository: a run has one
// integration ref, so a second repository would need its own result.
func resolveIsolationSource(ctx context.Context, d *graph.Definition, ids []string, manager WorkspaceManager) (WorkspaceSource, error) {
	var result WorkspaceSource
	resolved := map[string]WorkspaceSource{}
	for _, id := range ids {
		workspace, _ := d.Nodes[id].Config["workspace"].(string)
		source, seen := resolved[workspace]
		if !seen {
			var err error
			if source, err = manager.Preflight(ctx, workspace); err != nil {
				return WorkspaceSource{}, isolationFailure(d, id, fmt.Sprintf("node %q: workspace isolation preflight failed: %v", id, err))
			}
			resolved[workspace] = source
		}
		if result.Root != "" && result.Root != source.Root {
			return WorkspaceSource{}, isolationFailure(d, id, fmt.Sprintf("node %q: all isolated nodes must share one source repository", id))
		}
		result = source
	}
	return result, nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
