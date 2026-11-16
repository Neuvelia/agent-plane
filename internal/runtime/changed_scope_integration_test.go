package runtime_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/testenv"
)

func scopeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	testenv.RequireExecutable(t, "/usr/bin/git")
	c := exec.Command("/usr/bin/git", args...)
	c.Dir = dir
	c.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, b)
	}
}

func scopeRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	scopeGit(t, d, "init", "-q")
	if err := os.Mkdir(filepath.Join(d, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"src/a.go", "outside.go"} {
		if err := os.WriteFile(filepath.Join(d, p), []byte("initial"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	scopeGit(t, d, "add", ".")
	scopeGit(t, d, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	return d
}

func scopeDefinition(dir, mode string) *graph.Definition {
	return &graph.Definition{SchemaVersion: 0, ID: "scope", Units: []string{"work"}, Inputs: map[string]graph.InputDefinition{}, Edges: []graph.EdgeDefinition{}, Outputs: map[string]graph.OutputDefinition{}, Nodes: map[string]graph.NodeDefinition{
		"work": {Type: "agent", Config: graph.NodeConfig{"adapter": "fake", "task": "edit", "workspace": dir, "allowed_paths": []string{"src/**"}}, Outputs: map[string]graph.PortDefinition{"raw_metadata": {Type: graph.ValueTypeJSON, Required: true}}, Loop: &graph.LoopDefinition{MaxAttempts: 2, Gate: graph.LoopGateDefinition{Inputs: map[string]graph.PortDefinition{"candidate": {Type: graph.ValueTypeJSON, Required: true}}, Config: graph.NodeConfig{"check_id": "scope", "kind": "changed_files_scope", "input": "candidate", "mode": mode, "workspace": dir, "allowed_paths": []string{"src/**"}}}, Bindings: []graph.LoopBinding{{From: "candidate.outputs.raw_metadata", To: "gate.inputs.candidate"}}}},
	}}
}

func TestChangedScopeDenyRetriesAgainstFirstBaseline(t *testing.T) {
	dir := scopeRepo(t)
	a := newLoopAdapter()
	a.produce = func(_ context.Context, in agent.AgentExecutionInput, call int) (agent.AgentExecutionOutput, error) {
		if call == 1 {
			if err := os.WriteFile(filepath.Join(dir, "src/a.go"), []byte("allowed"), 0600); err != nil {
				return agent.AgentExecutionOutput{}, err
			}
			if err := os.WriteFile(filepath.Join(dir, "outside.go"), []byte("forbidden"), 0600); err != nil {
				return agent.AgentExecutionOutput{}, err
			}
			return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`), ChangedFiles: []agent.ChangedFile{{Path: "src/a.go", Provenance: agent.ProvenanceReported}}}, nil
		}
		if in.Feedback == nil || in.Feedback.CheckResult.Reason != "changed_files_outside_scope" {
			t.Fatal("missing scope feedback")
		}
		if err := os.WriteFile(filepath.Join(dir, "outside.go"), []byte("initial"), 0600); err != nil {
			return agent.AgentExecutionOutput{}, err
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`)}, nil
	}
	r, err := runtime.Run(context.Background(), scopeDefinition(dir, "deny"), nil, compose(a), runtime.Options{})
	if err != nil || r.State != runtime.RunSucceeded || len(a.calls) != 2 {
		t.Fatalf("run=%+v err=%v calls=%d", r, err, len(a.calls))
	}
	var red, green int
	for _, e := range r.Events {
		if e.Type != events.GateCheck {
			continue
		}
		p := e.Payload.(events.CheckResult)
		if p.Verdict == events.CheckRed {
			red++
			if p.Reason != "changed_files_outside_scope" || len(p.Evidence) == 0 || p.Evidence[0].Path != "outside.go" || p.ChangedFiles.Mode != "deny" || len(p.ChangedFiles.AllowedPaths) != 1 || p.ChangedFiles.AllowedPaths[0] != "src/" {
				t.Fatalf("red=%+v", p)
			}
		} else {
			green++
		}
	}
	if red != 1 || green != 1 {
		t.Fatalf("red=%d green=%d", red, green)
	}
}

func TestChangedScopeWarnDoesNotBlock(t *testing.T) {
	dir := scopeRepo(t)
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		if err := os.WriteFile(filepath.Join(dir, "outside.go"), []byte("forbidden"), 0600); err != nil {
			return agent.AgentExecutionOutput{}, err
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`), ChangedFiles: []agent.ChangedFile{}}, nil
	}
	r, err := runtime.Run(context.Background(), scopeDefinition(dir, "warn"), nil, compose(a), runtime.Options{})
	if err != nil || r.State != runtime.RunSucceeded || len(a.calls) != 1 {
		t.Fatalf("run=%+v err=%v", r, err)
	}
	var found bool
	for _, e := range r.Events {
		if e.Type == events.GateCheck {
			p := e.Payload.(events.CheckResult)
			found = p.Verdict == events.CheckGreen && p.Reason == "changed_files_outside_scope" && len(p.Evidence) > 0 && p.Evidence[0].Path == "outside.go" && p.ChangedFiles.Mode == "warn"
		}
	}
	if !found {
		t.Fatal("missing nonblocking scope evidence")
	}
}

func TestChangedScopeDuplicateRulesDoNotChangePolicy(t *testing.T) {
	dir := scopeRepo(t)
	d := scopeDefinition(dir, "deny")
	n := d.Nodes["work"]
	n.Config["allowed_paths"] = []string{"src/**", "src/"}
	d.Nodes["work"] = n
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`)}, nil
	}
	r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{})
	if err != nil || r.State != runtime.RunSucceeded {
		t.Fatalf("duplicate scope rejected: %v %+v", err, r)
	}
}

func TestChangedScopeEvidenceRemainsSortedAfterRedaction(t *testing.T) {
	dir := scopeRepo(t)
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		for _, name := range []string{"b.go", "z.go"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("new"), 0600); err != nil {
				return agent.AgentExecutionOutput{}, err
			}
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`)}, nil
	}
	r, err := runtime.Run(context.Background(), scopeDefinition(dir, "deny"), nil, compose(a), runtime.Options{LogRedactions: []string{"z"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Events {
		if e.Type == events.GateCheck {
			p := e.Payload.(events.CheckResult)
			paths := []string{}
			for _, v := range p.Evidence {
				if v.Path != "" {
					paths = append(paths, v.Path)
				}
			}
			if !sort.StringsAreSorted(paths) {
				t.Fatalf("unsorted redacted paths: %v", paths)
			}
			return
		}
	}
	t.Fatal("missing check")
}

func TestChangedScopeWarnStaysNonblockingWhenRedactionExpandsEvidence(t *testing.T) {
	dir := scopeRepo(t)
	a := newLoopAdapter()
	a.produce = func(_ context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
		folder := strings.Repeat("z", 200)
		if err := os.Mkdir(filepath.Join(dir, folder), 0700); err != nil {
			return agent.AgentExecutionOutput{}, err
		}
		for i := 0; i < 31; i++ {
			name := strings.Repeat("z", 200) + string(rune('a'+i))
			if err := os.WriteFile(filepath.Join(dir, folder, name), []byte("new"), 0600); err != nil {
				return agent.AgentExecutionOutput{}, err
			}
		}
		return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`)}, nil
	}
	r, err := runtime.Run(context.Background(), scopeDefinition(dir, "warn"), nil, compose(a), runtime.Options{LogRedactions: []string{"z"}})
	if err != nil || r.State != runtime.RunSucceeded {
		t.Fatalf("warn run blocked: %v failure=%+v", err, r.Failure)
	}
	for _, e := range r.Events {
		if e.Type == events.GateCheck {
			p := e.Payload.(events.CheckResult)
			if p.Reason != "changed_files_outside_scope" || p.Verdict != events.CheckGreen || p.Validate() != nil {
				t.Fatalf("invalid warn check: %+v", p)
			}
			if p.Evidence[len(p.Evidence)-1].Omitted == 0 {
				t.Fatalf("missing truncation count: %+v", p.Evidence)
			}
			return
		}
	}
	t.Fatal("missing check")
}

func TestChangedScopeGitFailureWarnAndDeny(t *testing.T) {
	for _, mode := range []string{"warn", "deny"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			a := newLoopAdapter()
			a.produce = func(_ context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
				return agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{}`)}, nil
			}
			r, err := runtime.Run(context.Background(), scopeDefinition(dir, mode), nil, compose(a), runtime.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "deny" {
				if r.State != runtime.RunFailed || len(a.calls) != 0 {
					t.Fatalf("deny passed Git failure: %+v", r)
				}
			} else {
				if r.State != runtime.RunSucceeded || len(a.calls) != 1 {
					t.Fatalf("warn blocked Git failure: %+v", r)
				}
				var seen bool
				for _, e := range r.Events {
					if e.Type == events.GateCheck {
						p := e.Payload.(events.CheckResult)
						seen = p.Reason == "changed_files_scope_indeterminate" && p.Verdict == events.CheckGreen
					}
				}
				if !seen {
					t.Fatal("warn uncertainty not traced")
				}
			}
		})
	}
}

func TestChangedScopeGateMustMatchAgentDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(g graph.NodeConfig, agentCfg graph.NodeConfig, other string)
	}{
		{"gate widens allowed_paths", func(g, _ graph.NodeConfig, _ string) { g["allowed_paths"] = []string{"src/**", "outside.go"} }},
		{"gate narrows allowed_paths", func(g, _ graph.NodeConfig, _ string) { g["allowed_paths"] = []string{"src/a.go"} }},
		{"gate workspace differs", func(g, _ graph.NodeConfig, other string) { g["workspace"] = other }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := scopeRepo(t)
			d := scopeDefinition(dir, "deny")
			n := d.Nodes["work"]
			tc.mutate(n.Loop.Gate.Config, n.Config, scopeRepo(t))
			d.Nodes["work"] = n
			a := newLoopAdapter()
			a.produce = func(_ context.Context, _ agent.AgentExecutionInput, _ int) (agent.AgentExecutionOutput, error) {
				t.Fatal("Agent executed despite mismatched Gate declaration")
				return agent.AgentExecutionOutput{}, nil
			}
			r, err := runtime.Run(context.Background(), d, nil, compose(a), runtime.Options{})
			if err == nil && r.State == runtime.RunSucceeded {
				t.Fatalf("mismatched Gate accepted: %+v", r)
			}
		})
	}
}
