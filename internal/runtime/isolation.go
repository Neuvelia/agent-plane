package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"agentplane/internal/domain/events"
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

// isolatedWorkspace is the coordinator-owned lifecycle state of one node's
// worktree. No worker goroutine reads or writes it.
type isolatedWorkspace struct {
	handle     Workspace
	path       string
	unitRef    string
	unitCommit string
	integrated bool
}

// isolationError carries a fixed, safe diagnostic code; the wrapped manager
// error stays local to the run result and never reaches an event.
type isolationError struct {
	code string
	err  error
}

func (e *isolationError) Error() string          { return e.err.Error() }
func (e *isolationError) Unwrap() error          { return e.err }
func (e *isolationError) DiagnosticCode() string { return e.code }

// prepareIsolated creates the node's worktree and records it. It runs on the
// coordinator, serially, so event order is deterministic.
func (m *runMachine) prepareIsolated(ctx context.Context, id string) error {
	handle, err := m.isolation.manager.Create(ctx, m.isolation.source, string(m.result.RunID), string(m.nodes[id].NodeRunID))
	if err != nil {
		return &isolationError{code: "E_WORKSPACE_CREATE", err: fmt.Errorf("creating isolated workspace for node %q: %w", id, err)}
	}
	m.worktrees[id] = &isolatedWorkspace{handle: handle, path: handle.Path()}
	m.appendEvent(events.Event{Type: events.WorkspacePrepared, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, Payload: events.WorkspacePreparedPayload{
		BaseSHA: m.isolation.source.BaseSHA, Mode: events.WorkspaceModeWorktree, Path: handle.Path(), SourceRoot: m.isolation.source.Root,
	}})
	return nil
}

// isolatedNode is the node a worker executes: the same definition with the
// Agent and its embedded Gate pointed at the worktree. The graph and its
// semantic version are never modified.
func isolatedNode(n graph.NodeDefinition, path string) graph.NodeDefinition {
	n.Config = cloneConfig(n.Config)
	n.Config["workspace"] = path
	if n.Loop != nil {
		loop := *n.Loop
		loop.Gate.Config = cloneConfig(loop.Gate.Config)
		if _, ok := loop.Gate.Config["workspace"]; ok {
			loop.Gate.Config["workspace"] = path
		}
		n.Loop = &loop
	}
	return n
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

// integrateIsolated snapshots an accepted unit and merges it onto the run's
// integration ref. It runs on the coordinator, in the sorted acceptance order,
// after the unit's values are frozen. It reports whether the node may succeed;
// on false the node has already been moved to its terminal state. Accepted
// values are never rewritten and no unit is rerun.
func (m *runMachine) integrateIsolated(ctx context.Context, id string, attempt events.AttemptID) bool {
	state := m.worktrees[id]
	fail := func(code, step string, err error) bool {
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			m.transitionNode(id, run.NodeCancelled)
			return false
		}
		m.nodes[id].Failure = &run.Failure{NodeID: id, Kind: run.FailureExecutor, Err: &isolationError{code: code, err: fmt.Errorf("%s for node %q: %w", step, id, err)}}
		m.transitionNode(id, run.NodeFailed)
		return false
	}
	runID := string(m.result.RunID)
	unitRef, err := m.isolation.manager.UnitRef(runID, id)
	if err != nil {
		return fail("E_WORKSPACE_SNAPSHOT", "naming the unit ref", err)
	}
	tipRef, err := m.isolation.manager.IntegratedRef(runID)
	if err != nil {
		return fail("E_WORKSPACE_INTEGRATE", "naming the integration ref", err)
	}
	snapshot, err := m.isolation.manager.Snapshot(ctx, state.handle, unitRef)
	if err != nil {
		return fail("E_WORKSPACE_SNAPSHOT", "snapshotting the isolated workspace", err)
	}
	state.unitRef, state.unitCommit = unitRef, snapshot.Commit
	result, err := m.isolation.manager.Integrate(ctx, m.isolation.source, tipRef, snapshot.Commit)
	if err != nil {
		return fail("E_WORKSPACE_INTEGRATE", "integrating the unit", err)
	}
	if result.Conflict {
		return fail("E_WORKSPACE_INTEGRATE", "integrating the unit", errors.New("unexpected conflict"))
	}
	state.integrated = true
	m.appendEvent(events.Event{Type: events.WorkspaceIntegrated, NodeID: id, NodeRunID: m.nodes[id].NodeRunID, AttemptID: attempt, Payload: events.WorkspaceIntegratedPayload{
		UnitRef: unitRef, UnitCommit: snapshot.Commit, IntegratedRef: tipRef, IntegratedCommit: result.TipCommit, ChangedPaths: nonNegative(result.ChangedPaths),
	}})
	return true
}

func nonNegative(n int) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}
