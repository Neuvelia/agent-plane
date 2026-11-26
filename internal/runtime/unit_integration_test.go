package runtime_test

import (
	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/testenv"
)

func unitGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
}
func unitRepo(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	unitGit(t, root, "init", "-q")
	unitGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "base")
	return root
}
func unitWorktrees(t *testing.T, n int) []string {
	t.Helper()
	root := unitRepo(t)
	roots := []string{root}
	for i := 1; i < n; i++ {
		p := filepath.Join(t.TempDir(), "worktree")
		unitGit(t, root, "worktree", "add", "--detach", "-q", p, "HEAD")
		p, e := filepath.EvalSymlinks(p)
		if e != nil {
			t.Fatal(e)
		}
		roots = append(roots, p)
	}
	return roots
}
func unitBatch(roots []string, budget int) *graph.Definition {
	d := loopDefinition(budget)
	delete(d.Nodes, "work")
	delete(d.Nodes, "down")
	d.Edges = []graph.EdgeDefinition{}
	names := []string{"a", "b", "c", "d"}
	for i, id := range names {
		n := loopDefinition(budget).Nodes["work"]
		n.Config["task"] = id
		n.Config["workspace"] = roots[i%len(roots)]
		n.Config["allowed_paths"] = []string{id + ".txt"}
		d.Nodes[id] = n
		d.Units = append(d.Units, id)
		down := "down_" + id
		d.Nodes[down] = graph.NodeDefinition{Type: "shell", Inputs: map[string]graph.PortDefinition{"result": {Type: graph.ValueTypeJSON, Required: true}}}
		d.Edges = append(d.Edges, graph.EdgeDefinition{From: graph.NodeOutputReference(id + ".outputs.raw_metadata"), To: graph.NodeInputReference(down + ".inputs.result")})
	}
	d.Nodes["join"] = graph.NodeDefinition{Type: "shell", Inputs: map[string]graph.PortDefinition{}}
	join := d.Nodes["join"]
	for _, id := range names {
		join.Inputs[id] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
		d.Edges = append(d.Edges, graph.EdgeDefinition{From: graph.NodeOutputReference(id + ".outputs.raw_metadata"), To: graph.NodeInputReference("join.inputs." + id)})
	}
	d.Nodes["join"] = join
	return d
}
func TestUnitsBarrierScopedRetryAndExhaustion(t *testing.T) {
	for _, budget := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			roots := unitWorktrees(t, 4)
			d := unitBatch(roots, budget)
			a := newLoopAdapter()
			entered := make(chan struct{}, 4)
			release := make(chan struct{})
			counts := map[string]int{}
			var mu sync.Mutex
			a.produce = func(ctx context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
				mu.Lock()
				counts[in.Task]++
				n := counts[in.Task]
				mu.Unlock()
				if n == 1 {
					entered <- struct{}{}
					select {
					case <-release:
					case <-ctx.Done():
						return agent.AgentExecutionOutput{}, ctx.Err()
					}
				}
				ok := in.Task != "c" || budget == 0 && n == 2
				raw := fmt.Sprintf(`{"ok":%t,"unit":%q,"attempt":%d}`, ok, in.Task, n)
				return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(raw)}, nil
			}
			e := compose(a)
			base := e
			down := map[string]int{}
			e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
				if n.Type == "shell" {
					mu.Lock()
					defer mu.Unlock()
					down[id]++
					for _, v := range in {
						b, _ := v.AsJSON()
						if !strings.Contains(string(b), `"ok":true`) {
							t.Error("red candidate leaked", string(b))
						}
					}
					return nil, nil, nil
				}
				return base.ExecuteNodeObserved(ctx, id, n, in, o)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type outcome struct {
				r run.Result
				e error
			}
			done := make(chan outcome, 1)
			go func() {
				r, err := runtime.Run(ctx, d, nil, e, runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 4})
				done <- outcome{r, err}
			}()
			for range 4 {
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("distinct worktrees did not overlap")
				}
			}
			close(release)
			result := <-done
			r := result.r
			if result.e != nil {
				t.Fatal(result.e)
			}
			expectedC := budget
			if budget == 0 {
				expectedC = 2
			}
			if counts["a"] != 1 || counts["b"] != 1 || counts["d"] != 1 || counts["c"] != expectedC {
				t.Fatal(counts)
			}
			if budget == 0 {
				if r.State != run.RunSucceeded || down["down_a"] != 1 || down["down_b"] != 1 || down["down_d"] != 1 || down["down_c"] != 1 || down["join"] != 1 {
					t.Fatal(r.State, down)
				}
			} else {
				if r.State != run.RunFailed || r.Failure.Kind != run.FailureNeedsIntervention || down["down_c"] != 0 || down["join"] != 0 {
					t.Fatal(r.State, r.Failure, down)
				}
			}
			accepted := map[string]events.ReturnEnvelopePayload{}
			var aGreen, cRed uint64
			checks := map[events.AttemptID]uint64{}
			envelopes := map[events.AttemptID]uint64{}
			for _, ev := range r.Events {
				if ev.Type == events.GateCheck {
					checks[ev.AttemptID] = ev.Sequence
				}
				if ev.Type == events.ReturnEnvelope {
					p := ev.Payload.(events.ReturnEnvelopePayload)
					envelopes[ev.AttemptID] = ev.Sequence
					if p.Verdict == "green" {
						if _, exists := accepted[ev.NodeID]; exists {
							t.Fatal("second accepted unit")
						}
						accepted[ev.NodeID] = p
						if ev.NodeID == "a" {
							aGreen = ev.Sequence
						}
					}
					if ev.NodeID == "c" && p.Verdict == "red" && cRed == 0 {
						cRed = ev.Sequence
					}
					linked := events.AttemptCheckResults(r.Events, r.RunID, ev.NodeRunID, ev.AttemptID)
					if len(linked) != 1 || !reflect.DeepEqual(linked[0].Evidence, p.Evidence) || linked[0].Reason != p.Reason || linked[0].Verdict != p.Verdict {
						t.Fatal("envelope check disagreement")
					}
				}
				if ev.Type == events.AttemptFinished {
					if checks[ev.AttemptID] >= envelopes[ev.AttemptID] || envelopes[ev.AttemptID] >= ev.Sequence || envelopes[ev.AttemptID] == 0 {
						t.Fatal("fact ordering", ev)
					}
				}
			}
			if aGreen == 0 || aGreen >= cRed {
				t.Fatal("A was not accepted before C rejection")
			}
			for _, n := range r.Nodes {
				if n.NodeID == "a" || n.NodeID == "b" || n.NodeID == "d" {
					p := accepted[n.NodeID]
					if n.AcceptedAttemptID != p.AttemptID || n.NodeRunID != p.SourceNodeRunID || len(n.Outputs) != 1 {
						t.Fatal(n, p)
					}
					provenance := n.Outputs[0].Provenance
					if provenance == nil || provenance.AttemptID != p.AttemptID || provenance.RunID != r.RunID || provenance.NodeRunID != n.NodeRunID || provenance.Port != "raw_metadata" {
						t.Fatal(provenance)
					}
					raw, _ := n.Outputs[0].Value.AsJSON()
					if string(raw) != fmt.Sprintf(`{"ok":true,"unit":%q,"attempt":1}`, n.NodeID) {
						t.Fatal(string(raw))
					}
				}
			}
		})
	}
}
func TestUnitsSharedFilesCanChangeAcceptedValuesCannot(t *testing.T) {
	root := unitRepo(t)
	d := unitBatch([]string{root}, 3)
	// Stable authored IDs put the rejected unit after already accepted siblings.
	c := d.Nodes["c"]
	delete(d.Nodes, "c")
	d.Nodes["z_c"] = c
	for i, id := range d.Units {
		if id == "c" {
			d.Units[i] = "z_c"
		}
	}
	for i, b := range d.Edges {
		if strings.HasPrefix(string(b.From), "c.outputs.") {
			d.Edges[i].From = graph.NodeOutputReference(strings.Replace(string(b.From), "c.outputs.", "z_c.outputs.", 1))
		}
	}
	a := newLoopAdapter()
	counts := map[string]int{}
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		counts[in.Task]++
		n := counts[in.Task]
		if in.Task == "a" {
			if e := os.WriteFile(filepath.Join(root, "a.txt"), []byte("accepted A file"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		if in.Task == "c" {
			if counts["a"] != 1 || counts["b"] != 1 || counts["d"] != 1 {
				t.Error("siblings not complete")
			}
			if e := os.WriteFile(filepath.Join(root, "a.txt"), []byte("C modified accepted A file"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(fmt.Sprintf(`{"ok":%t,"unit":%q,"attempt":%d}`, in.Task != "c" || n == 2, in.Task, n))}, nil
	}
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 4})
	if err != nil || r.State != run.RunSucceeded {
		t.Fatal(r, err)
	}
	content, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(content) != "C modified accepted A file" {
		t.Fatal("test did not exercise shared side effect")
	}
	for _, n := range r.Nodes {
		if n.NodeID == "a" {
			b, _ := n.Outputs[0].Value.AsJSON()
			if string(b) != `{"ok":true,"unit":"a","attempt":1}` || n.Outputs[0].Provenance.AttemptID != n.AcceptedAttemptID || n.AcceptedAttemptID == "" {
				t.Fatal(n)
			}
		}
	}
	if !reflect.DeepEqual(counts, map[string]int{"a": 1, "b": 1, "c": 2, "d": 1}) {
		t.Fatal(counts)
	}
}

func TestUnitsScopeValidationBeforeAdapterDetectionAndLegacyAbsence(t *testing.T) {
	for _, scope := range [][]string{{"../outside"}, {"/outside"}, {"a\\b"}, {"\x00"}, {strings.Repeat("a", 513)}, make([]string, 33)} {
		d := loopDefinition(1)
		d.Units = []string{"work"}
		d.Nodes["work"].Config["allowed_paths"] = scope
		a := newLoopAdapter()
		if _, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}}); err == nil || a.detect != 0 || len(a.calls) != 0 {
			t.Fatal("invalid scope reached detection", err, a.detect)
		}
	}
	a := newLoopAdapter()
	d := loopDefinition(1)
	r, e := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if e != nil {
		t.Fatal(e)
	}
	for _, ev := range r.Events {
		if ev.Type == events.ReturnEnvelope || ev.Type == events.DeclaredScopeConflicts {
			t.Fatal("legacy graph enrolled implicitly")
		}
	}
}
func TestUnitEnvelopeUsesCompleteNormalizedRedactedAuthoredScope(t *testing.T) {
	d := loopDefinition(1)
	d.Units = []string{"work"}
	d.Nodes["work"].Config["allowed_paths"] = []string{"./SECRET//a/", "."}
	a := newLoopAdapter()
	r, e := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"SECRET"}})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, ev := range r.Events {
		if ev.Type == events.ReturnEnvelope {
			found = true
			p := ev.Payload.(events.ReturnEnvelopePayload)
			if len(p.IntendedScope) != 2 || !p.ScopeRedacted || strings.Contains(p.IntendedScope[0], "SECRET") || !strings.HasSuffix(p.IntendedScope[0], "/a/") || p.IntendedScope[1] != "./" {
				t.Fatal(p)
			}
			if p.IntendedScope[0] == "candidate" {
				t.Fatal("gate checked ports replaced intended scope")
			}
		}
	}
	if !found {
		t.Fatal("missing envelope")
	}
	// Redaction can expand a complete authored scope beyond its representation
	// bound; fail before any adapter detection instead of truncating it.
	d.Nodes["work"].Config["allowed_paths"] = []string{strings.Repeat("S", 512)}
	a = newLoopAdapter()
	if _, e := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, LogRedactions: []string{"S"}}); e == nil || a.detect != 0 {
		t.Fatal("redaction bound not preflighted", e, a.detect)
	}
}
func TestUnitCancellationAndOperationalFailuresHaveNoInventedEnvelope(t *testing.T) {
	for _, mode := range []string{"provider", "cancel", "gate-error"} {
		t.Run(mode, func(t *testing.T) {
			a := newLoopAdapter()
			d := loopDefinition(3)
			d.Units = []string{"work"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := compose(a)
			base := e
			if mode == "provider" || mode == "cancel" {
				a.produce = func(context.Context, agent.AgentExecutionInput, int) (agent.AgentExecutionOutput, error) {
					if mode == "cancel" {
						cancel()
						return agent.AgentExecutionOutput{}, ctx.Err()
					}
					return agent.AgentExecutionOutput{}, fmt.Errorf("provider failed")
				}
			}
			if mode == "gate-error" {
				e.hook = func(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
					if n.Type == "gate" {
						return nil, nil, fmt.Errorf("gate failed")
					}
					return base.ExecuteNodeObserved(ctx, id, n, in, o)
				}
			}
			r, err := runtime.Run(ctx, d, nil, e, runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
			if err != nil || len(a.calls) != 1 {
				t.Fatal(r, err)
			}
			for _, ev := range r.Events {
				if ev.Type == events.ReturnEnvelope {
					t.Fatal("invented operational verdict")
				}
			}
			for _, n := range r.Nodes {
				if n.AcceptedAttemptID != "" {
					t.Fatal("operational error accepted")
				}
			}
		})
	}
}

func TestEnrolledUnitIdentityBoundBeforeDetectionPreservesLegacyIDs(t *testing.T) {
	for _, enrolled := range []bool{false, true} {
		a := newLoopAdapter()
		d := loopDefinition(1)
		id := strings.Repeat("u", 257)
		d.Nodes[id] = d.Nodes["work"]
		delete(d.Nodes, "work")
		d.Edges[0].From = graph.NodeOutputReference(id + ".outputs.raw_metadata")
		if enrolled {
			d.Units = []string{id}
		}
		r, e := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
		if enrolled {
			if e == nil || a.detect != 0 || len(a.calls) != 0 {
				t.Fatal("oversized unit ID reached provider", e, a.detect)
			}
		} else if e != nil || a.detect == 0 || len(a.calls) != 1 || r.Failure.Kind != run.FailureNeedsIntervention {
			t.Fatal("legacy ID narrowed", r, e, a.detect)
		}
	}
}

func TestUnitEnvelopeRetainsScopeBeyondLegacyAttemptSummary(t *testing.T) {
	d := loopDefinition(1)
	d.Units = []string{"work"}
	scope := []string{}
	for i := 0; i < 7; i++ {
		scope = append(scope, fmt.Sprintf("dir%d/", i)+strings.Repeat("x", 251))
	}
	d.Nodes["work"].Config["allowed_paths"] = scope
	a := newLoopAdapter()
	r, e := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}})
	if e != nil {
		t.Fatal(e)
	}
	envelopeSeen, summaryOmitted := false, false
	for _, ev := range r.Events {
		switch p := ev.Payload.(type) {
		case events.ReturnEnvelopePayload:
			envelopeSeen = true
			if !reflect.DeepEqual(p.IntendedScope, scope) {
				t.Fatal("complete envelope scope truncated", p.IntendedScope)
			}
		case events.AttemptInputPayload:
			summaryOmitted = p.ScopeOmitted
		}
	}
	if !envelopeSeen || !summaryOmitted || !reflect.DeepEqual(a.calls[0].AllowedPaths, scope) {
		t.Fatal("full enrolled intent or legacy summary behavior lost")
	}
}
