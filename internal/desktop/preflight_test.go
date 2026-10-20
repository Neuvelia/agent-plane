package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/graph"
)

func preflightFixture(t *testing.T, graphText string) (*PreflightService, Document, WorkspaceRecord, string) {
	t.Helper()
	registry, stateDir, projectDir := registryFixture(t)
	grant, err := registry.GrantProjectFolder(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	project, err := registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	workspaceDir := filepath.Join(projectDir, "workspace")
	if err := os.Mkdir(workspaceDir, 0700); err != nil {
		t.Fatal(err)
	}
	workspace, err := registry.AddWorkspace(project.ProjectID, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(projectDir, "graph.yaml")
	if err := os.WriteFile(graphPath, []byte(graphText), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewDocumentStore(registry, stateDir)
	doc, err := store.Open(project.ProjectID, "graph.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return NewPreflightService(registry, store, nil), doc, workspace, graphPath
}

func TestPreflightShellGraphRequiresSavedValidRevisionWithoutStartingCommand(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "would-run")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(`schema_version: 0
id: safe
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: SCRIPT
    outputs: {}
edges: []
outputs: {}
`, "SCRIPT", script, 1)
	service, doc, workspace, _ := preflightFixture(t, text)
	result, err := service.Preflight(context.Background(), PreflightRequest{DocumentID: doc.DocumentID,
		Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1})
	if err != nil || result.HasErrors || result.PreflightToken == "" || result.GraphVersion == "" || result.PayloadDigest == "" {
		t.Fatalf("shell preflight = %#v, %v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("preflight ran graph command: %v", err)
	}
	changed, err := service.documents.Update(doc.DocumentID, doc.Revision, strings.Replace(text, "id: safe", "id: changed", 1))
	if err != nil {
		t.Fatal(err)
	}
	result, err = service.Preflight(context.Background(), PreflightRequest{DocumentID: changed.DocumentID,
		Revision: changed.Revision, WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1})
	if err != nil || !result.HasErrors || result.PreflightToken != "" {
		t.Fatalf("unsaved draft preflight = %#v, %v", result, err)
	}
}

func TestPreflightRejectsGraphInputContractViolations(t *testing.T) {
	text := `schema_version: 0
id: inputs
inputs:
  required: {type: string, required: true}
nodes: {}
edges: []
outputs: {}
`
	service, doc, workspace, _ := preflightFixture(t, text)
	request := PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	for _, input := range []map[string]graph.Value{
		{},
		{"required": graph.NewBooleanValue(true)},
		{"required": graph.NewStringValue("ok"), "unknown": graph.NewStringValue("bad")},
	} {
		request.Inputs = input
		result, err := service.Preflight(context.Background(), request)
		if err != nil || !result.HasErrors || result.PreflightToken != "" {
			t.Fatalf("invalid inputs preflight = %#v, %v", result, err)
		}
	}
	request.Inputs = map[string]graph.Value{"required": graph.NewStringValue("ok")}
	result, err := service.Preflight(context.Background(), request)
	if err != nil || result.HasErrors || result.PreflightToken == "" {
		t.Fatalf("valid inputs preflight = %#v, %v", result, err)
	}
}

func TestPreflightWarningsDoNotBlockAndTokenExpires(t *testing.T) {
	text := `schema_version: 0
id: warning
inputs: {}
nodes:
  unused:
    type: shell
    config: {command: /bin/echo}
    outputs:
      stdout: {type: string}
edges: []
outputs: {}
`
	service, doc, workspace, _ := preflightFixture(t, text)
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	request := PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	result, err := service.Preflight(context.Background(), request)
	if err != nil || result.HasErrors || result.PreflightToken == "" || result.DiagnosticsTotal == 0 {
		t.Fatalf("warning preflight = %#v, %v", result, err)
	}
	if _, ok := service.ticket(result.PreflightToken); !ok {
		t.Fatal("fresh token unavailable")
	}
	now = now.Add(preflightTokenLifetime)
	if _, ok := service.ticket(result.PreflightToken); ok {
		t.Fatal("expired token still available")
	}
}

func TestPreflightRejectsWorkspaceFromDifferentProject(t *testing.T) {
	service, doc, _, _ := preflightFixture(t, validGraphText)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	grant, err := service.registry.GrantProjectFolder(other)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.registry.AddProject(grant.Token, grant.PathDigest)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.registry.AddWorkspace(project.ProjectID, ".")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Preflight(context.Background(), PreflightRequest{DocumentID: doc.DocumentID,
		Revision: doc.Revision, WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1})
	if err != nil || !result.HasErrors || result.PreflightToken != "" || result.Diagnostics[0].Code != "E_WORKSPACE_PROJECT" {
		t.Fatalf("cross-project workspace = %#v, %v", result, err)
	}
}

func TestPreflightPayloadDigestUsesExactTypedValues(t *testing.T) {
	service, doc, workspace, _ := preflightFixture(t, `schema_version: 0
id: exact
inputs:
  a: {type: json}
  b: {type: number}
nodes: {}
edges: []
outputs: {}
`)
	jsonA, _ := graph.NewJSONValue([]byte(`{"x":1,"y":2}`))
	jsonB, _ := graph.NewJSONValue([]byte(`{"y":2,"x":1}`))
	number, _ := graph.NewNumberValue("1.0")
	request := PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision,
		WorkspaceID: workspace.WorkspaceID, MaxConcurrency: 1,
		Inputs: map[string]graph.Value{"a": jsonA, "b": number}}
	first, err := service.Preflight(context.Background(), request)
	if err != nil || first.HasErrors {
		t.Fatalf("first preflight = %#v, %v", first, err)
	}
	request.Inputs = map[string]graph.Value{"b": number, "a": jsonA}
	ordered, err := service.Preflight(context.Background(), request)
	if err != nil || ordered.PayloadDigest != first.PayloadDigest {
		t.Fatalf("map order altered digest: %#v, %v", ordered, err)
	}
	request.Inputs["a"] = jsonB
	changedJSON, err := service.Preflight(context.Background(), request)
	if err != nil || changedJSON.PayloadDigest == first.PayloadDigest {
		t.Fatalf("JSON byte change preserved digest: %#v, %v", changedJSON, err)
	}
	request.Inputs["a"] = jsonA
	request.Inputs["b"], _ = graph.NewNumberValue("1")
	changedNumber, err := service.Preflight(context.Background(), request)
	if err != nil || changedNumber.PayloadDigest == first.PayloadDigest {
		t.Fatalf("number spelling preserved digest: %#v, %v", changedNumber, err)
	}
}

type fakeCapabilities struct {
	report      agent.CapabilityReport
	detectCalls int
	gitCalls    int
}

func (f *fakeCapabilities) DetectCodex(context.Context, string) (agent.CapabilityReport, error) {
	f.detectCalls++
	return f.report, nil
}

func (f *fakeCapabilities) CheckGit(context.Context, string, string) error {
	f.gitCalls++
	return nil
}

func TestPreflightBindsDetectedAgentCapabilityAndSettings(t *testing.T) {
	service, initial, workspace, _ := preflightFixture(t, validGraphText)
	resolved, err := service.registry.ResolveWorkspace(workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	graphText := fmt.Sprintf(`schema_version: 0
id: agent
inputs: {}
nodes:
  work:
    type: agent
    config:
      adapter: codex
      task: inspect
      workspace: %q
    outputs: {}
edges: []
outputs: {}
`, resolved.CanonicalPath)
	updated, err := service.documents.Update(initial.DocumentID, initial.Revision, graphText)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.documents.Save(updated.DocumentID, updated.Revision, updated.DiskDigest, false)
	if err != nil {
		t.Fatal(err)
	}
	request := PreflightRequest{DocumentID: saved.DocumentID, Revision: saved.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	missing, err := service.Preflight(context.Background(), request)
	if err != nil || !missing.HasErrors || missing.PreflightToken != "" {
		t.Fatalf("missing tools = %#v, %v", missing, err)
	}
	toolDir := t.TempDir()
	codexPath := filepath.Join(toolDir, "codex")
	gitPath := filepath.Join(toolDir, "git")
	for _, path := range []string{codexPath, gitPath} {
		if err := os.WriteFile(path, []byte("fake executable"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.registry.UpdateSettings(1, SettingsPatch{CodexBinaryPath: OptionalPath{Set: true, Value: &codexPath},
		GitBinaryPath: OptionalPath{Set: true, Value: &gitPath}}); err != nil {
		t.Fatal(err)
	}
	trueValue := true
	version := "1.0.0"
	fake := &fakeCapabilities{report: agent.CapabilityReport{Available: true, Workspace: &trueValue,
		CorrectionFeedback: &trueValue, Version: &version}}
	service.detector = fake
	first, err := service.Preflight(context.Background(), request)
	if err != nil || first.HasErrors || first.PreflightToken == "" || fake.detectCalls != 1 || fake.gitCalls != 1 {
		t.Fatalf("capable preflight = %#v, %v; calls %d/%d", first, err, fake.detectCalls, fake.gitCalls)
	}
	version = "1.0.1"
	second, err := service.Preflight(context.Background(), request)
	if err != nil || second.HasErrors || second.PayloadDigest == first.PayloadDigest {
		t.Fatalf("changed capability = %#v, %v", second, err)
	}
	if err := os.WriteFile(codexPath, []byte("new fake executable"), 0700); err != nil {
		t.Fatal(err)
	}
	changedBinary, err := service.Preflight(context.Background(), request)
	if err != nil || changedBinary.HasErrors || changedBinary.PayloadDigest == second.PayloadDigest {
		t.Fatalf("changed executable content = %#v, %v", changedBinary, err)
	}
	if _, serviceErr := service.PrepareStart(context.Background(), RunStartRequest{
		PreflightToken: second.PreflightToken, PayloadDigest: second.PayloadDigest,
		DocumentID: saved.DocumentID, Revision: saved.Revision, WorkspaceID: workspace.WorkspaceID,
		Inputs: request.Inputs, MaxConcurrency: 1,
	}); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("changed executable accepted by Start: %v", serviceErr)
	}
	request.MaxConcurrency = 5
	tooHigh, err := service.Preflight(context.Background(), request)
	if err != nil || !tooHigh.HasErrors || tooHigh.PreflightToken != "" {
		t.Fatalf("over settings limit = %#v, %v", tooHigh, err)
	}
}

func TestPreflightRejectsUnsupportedAdapterAndRemovedWorkspace(t *testing.T) {
	service, doc, workspace, _ := preflightFixture(t, validGraphText)
	resolved, err := service.registry.ResolveWorkspace(workspace.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf(`schema_version: 0
id: unsupported
inputs: {}
nodes:
  work:
    type: agent
    config: {adapter: unknown, task: inspect, workspace: %q}
    outputs: {}
edges: []
outputs: {}
`, resolved.CanonicalPath)
	updated, err := service.documents.Update(doc.DocumentID, doc.Revision, text)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.documents.Save(updated.DocumentID, updated.Revision, updated.DiskDigest, false)
	if err != nil {
		t.Fatal(err)
	}
	request := PreflightRequest{DocumentID: saved.DocumentID, Revision: saved.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	result, err := service.Preflight(context.Background(), request)
	if err != nil || !result.HasErrors || result.PreflightToken != "" || !hasDiagnosticCode(result.Diagnostics, "E_ADAPTER_UNSUPPORTED") {
		t.Fatalf("unsupported adapter = %#v, %v", result, err)
	}
	if err := os.Remove(resolved.CanonicalPath); err != nil {
		t.Fatal(err)
	}
	result, err = service.Preflight(context.Background(), request)
	if err != nil || !result.HasErrors || result.PreflightToken != "" || !hasDiagnosticCode(result.Diagnostics, "E_WORKSPACE_UNAVAILABLE") {
		t.Fatalf("removed workspace = %#v, %v", result, err)
	}
}

func hasDiagnosticCode(findings []DiagnosticDTO, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestPreflightTokenCountAndByteEviction(t *testing.T) {
	service := NewPreflightService(nil, nil, nil)
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	var tokens []string
	for i := 0; i < maxPreflightTokens+1; i++ {
		token, err := service.storeToken(PreflightTicket{Document: SavedDocumentSnapshot{Text: "small"}})
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	if _, ok := service.ticket(tokens[0]); ok || len(service.tokens) != maxPreflightTokens {
		t.Fatalf("count eviction failed: oldest=%v count=%d", ok, len(service.tokens))
	}
	service.tokens = make(map[string]PreflightTicket)
	service.bytes = 0
	large := strings.Repeat("x", 4<<20)
	tokens = nil
	for i := 0; i < 5; i++ {
		token, err := service.storeToken(PreflightTicket{Document: SavedDocumentSnapshot{Text: large}})
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	if _, ok := service.ticket(tokens[0]); ok || service.bytes > maxPreflightSnapshotBytes || len(service.tokens) != 4 {
		t.Fatalf("byte eviction failed: oldest=%v bytes=%d count=%d", ok, service.bytes, len(service.tokens))
	}
}

func TestPreflightStaticGateAndLoopCapability(t *testing.T) {
	service := NewPreflightService(nil, nil, &fakeCapabilities{})
	workspace := ResolvedWorkspace{CanonicalPath: "/tmp/workspace"}
	badGate := &graph.Definition{ID: "static", Nodes: map[string]graph.NodeDefinition{
		"check": {Type: "gate", Config: graph.NodeConfig{"kind": "unsupported"}},
	}}
	_, findings := service.checkNodes(context.Background(), badGate, workspace, defaultSettings())
	if !hasGraphDiagnosticCode(findings, "E_NODE_CONFIG") {
		t.Fatalf("invalid Gate config passed: %#v", findings)
	}
	trueValue, falseValue := true, false
	fake := &fakeCapabilities{report: agent.CapabilityReport{Available: true, Workspace: &trueValue,
		CorrectionFeedback: &falseValue}}
	service.detector = fake
	toolDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	codexPath, gitPath := filepath.Join(toolDir, "codex"), filepath.Join(toolDir, "git")
	for _, path := range []string{codexPath, gitPath} {
		if err := os.WriteFile(path, []byte("fake tool"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	settings := defaultSettings()
	settings.CodexBinaryPath, settings.GitBinaryPath = &codexPath, &gitPath
	malformed := &graph.Definition{ID: "loop", Nodes: map[string]graph.NodeDefinition{
		"work": {Type: "agent", Config: graph.NodeConfig{"adapter": "codex", "task": "inspect", "workspace": workspace.CanonicalPath},
			Loop: &graph.LoopDefinition{Gate: graph.LoopGateDefinition{Config: graph.NodeConfig{"kind": "unsupported"}}}},
	}}
	_, findings = service.checkNodes(context.Background(), malformed, workspace, settings)
	if !hasGraphDiagnosticCode(findings, "E_LOOP_PREFLIGHT") || fake.detectCalls != 0 || fake.gitCalls != 0 {
		t.Fatalf("malformed loop triggered probes: %#v, calls %d/%d", findings, fake.detectCalls, fake.gitCalls)
	}
	loop := &graph.Definition{ID: "loop", Nodes: map[string]graph.NodeDefinition{
		"work": {Type: "agent", Config: graph.NodeConfig{"adapter": "codex", "task": "inspect", "workspace": workspace.CanonicalPath},
			Loop: &graph.LoopDefinition{Gate: graph.LoopGateDefinition{
				Inputs: map[string]graph.PortDefinition{"candidate": {Type: graph.ValueTypeJSON, Required: true}},
				Config: graph.NodeConfig{"check_id": "quality", "kind": "json_predicate", "input": "candidate",
					"field": []string{"ok"}, "operator": "eq", "expected": true},
			}},
		},
	}}
	_, findings = service.checkNodes(context.Background(), loop, workspace, settings)
	if !hasGraphDiagnosticCode(findings, "E_LOOP_PREFLIGHT") || fake.detectCalls != 1 || fake.gitCalls != 1 {
		t.Fatalf("missing loop capability passed: %#v, calls %d/%d", findings, fake.detectCalls, fake.gitCalls)
	}
}

func TestInstalledGitCheckRejectsConfiguredRuntimeMismatch(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(tool, []byte("not git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := (installedCapabilities{}).CheckGit(context.Background(), t.TempDir(), tool); err == nil {
		t.Fatal("configured Git path differed from runtime path but was accepted")
	}
}

func hasGraphDiagnosticCode(findings []graph.Diagnostic, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
