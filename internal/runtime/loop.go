package runtime

import (
	"agentplane/internal/domain/run"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/gitstate"
)

// LoopPreflight is optional for ordinary graphs, required for loop composition.
// All static validation completes before any provider capability detection.
type LoopPreflight interface {
	ValidateNodeConfig(string, graph.NodeDefinition) error
	PreflightNode(context.Context, string, graph.NodeDefinition) error
}

func preflightLoops(ctx context.Context, d *graph.Definition, e Executor) error {
	ids := make([]string, 0)
	for _, id := range sortedDefinitionIDs(d) {
		if d.Nodes[id].Loop != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	fail := func(id string, err error) error {
		return &run.PreflightError{Diagnostics: graph.ValidationResult{Diagnostics: []graph.Diagnostic{{Severity: graph.SeverityError, Code: "E_LOOP_PREFLIGHT", GraphID: d.ID, Location: graph.DiagnosticLocation{NodeID: id, Path: "nodes." + id + ".loop"}, Message: err.Error(), Suggestion: "use valid Agent/Gate configuration and a correction-feedback-capable executor"}}}}
	}
	p, ok := e.(LoopPreflight)
	if !ok {
		return fail(ids[0], errors.New("loop executor does not support preflight"))
	}
	if _, ok := e.(ObservedExecutor); !ok {
		return fail(ids[0], errors.New("loop executor requires check observations"))
	}
	for _, id := range ids {
		n := d.Nodes[id]
		if err := p.ValidateNodeConfig(id, n); err != nil {
			return fail(id, err)
		}
		if err := p.ValidateNodeConfig(id, n.Loop.GateNode()); err != nil {
			return fail(id, err)
		}
		if n.Loop.Gate.Config["kind"] == "changed_files_scope" {
			gateWorkspace, _ := n.Loop.Gate.Config["workspace"].(string)
			agentWorkspace, _ := n.Config["workspace"].(string)
			if gateWorkspace != agentWorkspace {
				return fail(id, errors.New("changed-files Gate workspace must equal Agent workspace"))
			}
			gateScope := originalScope(n.Loop.GateNode())
			agentScope := originalScope(n)
			normalize := func(paths []string) ([]string, error) {
				out, err := events.NormalizeUnitScope(paths)
				for i, p := range out {
					if strings.HasSuffix(p, "/**") {
						out[i] = strings.TrimSuffix(p, "**")
					}
				}
				sort.Strings(out)
				return slices.Compact(out), err
			}
			a, ea := normalize(agentScope)
			g, eg := normalize(gateScope)
			if ea != nil || eg != nil || !slices.Equal(a, g) {
				return fail(id, errors.New("changed-files Gate allowed_paths must equal Agent allowed_paths"))
			}
		}
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := p.PreflightNode(ctx, id, d.Nodes[id]); err != nil {
			if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				return nil
			}
			return fail(id, err)
		}
	}
	return nil
}
func sortedDefinitionIDs(d *graph.Definition) []string {
	ids := make([]string, 0, len(d.Nodes))
	for id := range d.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func originalScope(n graph.NodeDefinition) []string {
	result := []string{}
	switch a := n.Config["allowed_paths"].(type) {
	case []string:
		result = append(result, a...)
	case []any:
		for _, v := range a {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
	}
	return result
}
func attemptInput(n graph.NodeDefinition, in map[string]graph.Value, f *run.CorrectionFeedback, r *logRedactor) events.AttemptInputPayload {
	task, _ := n.Config["task"].(string)
	if task == "" {
		task, _ = in["task"].AsString()
	}
	p := events.AttemptInputPayload{TaskFingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte(task))), ScopePresent: true, IntendedScope: []string{}}
	_, p.ContextRefsPresent = n.Config["context_refs"]
	_, p.EnvironmentRefsPresent = n.Config["environment_refs"]
	p.ReferencesOmitted = p.ContextRefsPresent || p.EnvironmentRefsPresent
	size := 0
	for _, s := range originalScope(n) {
		c := r.clone()
		b := append([]byte{}, c.Write(events.Stdout, []byte(strings.ToValidUTF8(s, "")))...)
		b = append(b, c.Flush(events.Stdout)...)
		s = string(b)
		if len(s) > 512 || size+len(s) > 1024 || len(p.IntendedScope) >= 32 {
			p.ScopeOmitted = true
			continue
		}
		size += len(s)
		p.IntendedScope = append(p.IntendedScope, s)
	}
	if f != nil {
		p.PreviousAttemptID = f.PreviousAttemptID
		p.PreviousCheckID = f.CheckResult.CheckID
	}
	return p
}

//nolint:gocognit,gocyclo,funlen // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func (m *runMachine) cycle(ctx context.Context, e Executor, id string, n graph.NodeDefinition, in map[string]graph.Value, aid events.AttemptID, number int, f *run.CorrectionFeedback, baseline *gitstate.Baseline, carried DiffSnapshot, r *logRedactor) completedNode {
	if n.Type == "human" {
		return m.humanNode(ctx, id, n, in, r)
	}
	if err := ctx.Err(); err != nil {
		return completedNode{id: id, attemptID: aid, err: err}
	}
	var baselineError error
	if n.Loop != nil && n.Loop.Gate.Config["kind"] == "changed_files_scope" && baseline == nil {
		workspace, _ := n.Config["workspace"].(string)
		captured, err := gitstate.Capture(ctx, workspace)
		if err != nil {
			if n.Loop.Gate.Config["mode"] != "warn" {
				return completedNode{id: id, attemptID: aid, err: fmt.Errorf("capture changed-files baseline: %w", err)}
			}
			baselineError = err
		} else {
			baseline = &captured
		}
	}
	work := m.produceWithDiff(ctx, n, carried, func() completedNode {
		return m.invoke(ctx, e, id, n, in, aid, number, f, baseline, baselineError, r)
	})
	work.baseline = baseline
	if n.Loop == nil {
		return work
	}
	// Agent producers must not fabricate checks; candidate validation precedes Gate execution.
	if work.check != nil || work.checkInvalid {
		work.err = errors.New("producer emitted Gate observation")
		work.check = nil
		work.checkInvalid = true
		return work
	}
	if work.err != nil {
		work.evidence = nil
		return work
	}
	work.diagnostics = validateNodeOutputs(m.result.GraphID, id, n.Outputs, work.outputs)
	if len(work.diagnostics) != 0 {
		work.err = errors.New("candidate violates declared output contract")
		return work
	}
	if err := ctx.Err(); err != nil {
		work.err = err
		return work
	}
	bound := map[string]graph.Value{}
	for _, b := range n.Loop.Bindings {
		parts := strings.Split(b.From, ".")
		target := strings.Split(b.To, ".")[2]
		values := in
		if parts[0] == "candidate" {
			values = work.outputs
		}
		if v, ok := values[parts[2]]; ok {
			bound[target] = v
		} else if n.Loop.Gate.Inputs[target].Required {
			work.err = errors.New("required loop Gate binding has no candidate value")
			return work
		}
	}
	gate := m.invoke(ctx, e, id, n.Loop.GateNode(), bound, aid, 0, nil, baseline, baselineError, r)
	valid := !gate.checkInvalid && gate.check != nil && gate.evidence == nil
	if valid && gate.check.Verdict == "red" {
		var v interface{ GateVerdict() string }
		valid = errors.As(gate.err, &v) && v.GateVerdict() == "red" && len(gate.outputs) == 0 && !errors.Is(gate.err, context.Canceled) && !errors.Is(gate.err, context.DeadlineExceeded)
	}
	if valid && gate.check.Verdict == "green" {
		valid = gate.err == nil && len(gate.outputs) == 0
	}
	if !valid {
		work.err = gate.err
		if work.err == nil || gate.check != nil || gate.checkInvalid {
			work.err = errors.New("invalid or inconsistent embedded Gate observation")
		}
		return work
	}
	work.check = gate.check
	work.err = gate.err
	return work
}

type interventionError struct{}

func (interventionError) Error() string {
	return "correction attempt budget exhausted; intervention required"
}
func (interventionError) DiagnosticCode() string { return "E_LOOP_NEEDS_INTERVENTION" }

func checkScopeMatches(n graph.NodeDefinition, p events.CheckResult) bool {
	var expected []string
	if p.Kind == "command" {
		a, _ := n.Config["left_input"].(string)
		b, _ := n.Config["right_input"].(string)
		expected = []string{a}
		if a != b {
			expected = append(expected, b)
		}
	} else {
		a, _ := n.Config["input"].(string)
		expected = []string{a}
	}
	sort.Strings(expected)
	return slices.Equal(expected, p.Scope.CheckedInputs)
}
