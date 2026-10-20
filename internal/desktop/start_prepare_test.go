package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/graph"
)

func TestPrepareStartRequiresExactSavedFileAndTypedPayload(t *testing.T) {
	service, doc, workspace, graphPath := preflightFixture(t, validGraphText)
	request := PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	preflight, err := service.Preflight(context.Background(), request)
	if err != nil || preflight.HasErrors || preflight.PreflightToken == "" {
		t.Fatalf("preflight: %#v, %v", preflight, err)
	}
	start := RunStartRequest{PreflightToken: preflight.PreflightToken, PayloadDigest: preflight.PayloadDigest,
		DocumentID: request.DocumentID, Revision: request.Revision, WorkspaceID: request.WorkspaceID,
		Inputs: request.Inputs, MaxConcurrency: request.MaxConcurrency}
	prepared, serviceErr := service.PrepareStart(context.Background(), start)
	if serviceErr != nil || prepared.Definition == nil || prepared.GraphVersion != preflight.GraphVersion {
		t.Fatalf("prepared: %#v, %v", prepared, serviceErr)
	}
	start.PayloadDigest = strings.Repeat("0", len(preflight.PayloadDigest))
	if _, serviceErr := service.PrepareStart(context.Background(), start); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("altered payload accepted: %v", serviceErr)
	}
	start.PayloadDigest = preflight.PayloadDigest
	data, err := os.ReadFile(graphPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(graphPath), "replacement.yaml")
	if err := os.WriteFile(replacement, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, graphPath); err != nil {
		t.Fatal(err)
	}
	if _, serviceErr := service.PrepareStart(context.Background(), start); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("same-byte file replacement accepted: %v", serviceErr)
	}
}

func TestPrepareStartRejectsChangedShellEnvironment(t *testing.T) {
	t.Setenv("AGENTPLANE_FROZEN_TEST", "first-value")
	graphText := `schema_version: 0
id: env_test
inputs: {}
nodes:
  step:
    type: shell
    config:
      command: /usr/bin/env
      env_allowlist: [AGENTPLANE_FROZEN_TEST]
    outputs: {}
edges: []
outputs: {}
`
	service, doc, workspace, _ := preflightFixture(t, graphText)
	request := PreflightRequest{DocumentID: doc.DocumentID, Revision: doc.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	preflight, err := service.Preflight(context.Background(), request)
	if err != nil || preflight.HasErrors || preflight.PreflightToken == "" {
		t.Fatalf("preflight: %#v, %v", preflight, err)
	}
	start := RunStartRequest{PreflightToken: preflight.PreflightToken, PayloadDigest: preflight.PayloadDigest,
		DocumentID: request.DocumentID, Revision: request.Revision, WorkspaceID: request.WorkspaceID,
		Inputs: request.Inputs, MaxConcurrency: request.MaxConcurrency}
	t.Setenv("AGENTPLANE_FROZEN_TEST", "second-value")
	if _, serviceErr := service.PrepareStart(context.Background(), start); serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("changed environment accepted: %v", serviceErr)
	}
}

type pausingCapabilities struct {
	entered chan struct{}
	release chan struct{}
	report  agent.CapabilityReport
}

func (p *pausingCapabilities) DetectCodex(ctx context.Context, _ string) (agent.CapabilityReport, error) {
	close(p.entered)
	select {
	case <-p.release:
		return p.report, nil
	case <-ctx.Done():
		return agent.CapabilityReport{}, ctx.Err()
	}
}
func (*pausingCapabilities) CheckGit(context.Context, string, string) error { return nil }

func TestPrepareStartRejectsDocumentEditDuringCapabilityProbe(t *testing.T) {
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
    config: {adapter: codex, task: inspect, workspace: %q}
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
	toolDir := t.TempDir()
	codexPath, gitPath := filepath.Join(toolDir, "codex"), filepath.Join(toolDir, "git")
	for _, path := range []string{codexPath, gitPath} {
		if err := os.WriteFile(path, []byte("fake executable"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.registry.UpdateSettings(1, SettingsPatch{CodexBinaryPath: OptionalPath{Set: true, Value: &codexPath},
		GitBinaryPath: OptionalPath{Set: true, Value: &gitPath}}); err != nil {
		t.Fatal(err)
	}
	truth := true
	report := agent.CapabilityReport{Available: true, Workspace: &truth, CorrectionFeedback: &truth}
	service.detector = &fakeCapabilities{report: report}
	request := PreflightRequest{DocumentID: saved.DocumentID, Revision: saved.Revision,
		WorkspaceID: workspace.WorkspaceID, Inputs: map[string]graph.Value{}, MaxConcurrency: 1}
	result, err := service.Preflight(context.Background(), request)
	if err != nil || result.HasErrors || result.PreflightToken == "" {
		t.Fatalf("preflight = %#v, %v", result, err)
	}
	pausing := &pausingCapabilities{entered: make(chan struct{}), release: make(chan struct{}), report: report}
	service.detector = pausing
	start := RunStartRequest{PreflightToken: result.PreflightToken, PayloadDigest: result.PayloadDigest,
		DocumentID: saved.DocumentID, Revision: saved.Revision, WorkspaceID: workspace.WorkspaceID,
		Inputs: request.Inputs, MaxConcurrency: 1}
	done := make(chan *ServiceError, 1)
	go func() { _, serviceErr := service.PrepareStart(context.Background(), start); done <- serviceErr }()
	<-pausing.entered
	if _, err := service.documents.Update(saved.DocumentID, saved.Revision, strings.Replace(graphText, "id: agent", "id: edited", 1)); err != nil {
		t.Fatal(err)
	}
	close(pausing.release)
	if serviceErr := <-done; serviceErr == nil || serviceErr.Code != "conflict" {
		t.Fatalf("edit during capability probe accepted: %v", serviceErr)
	}
}
