package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/agent/adaptertest"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

func agentGraph(workspace string) *graph.Definition {
	return &graph.Definition{SchemaVersion: graph.SchemaVersionV0, ID: "agent-contract", Inputs: map[string]graph.InputDefinition{"job": {Type: graph.ValueTypeString, Required: true}}, Nodes: map[string]graph.NodeDefinition{
		"work": {Type: "agent", Inputs: map[string]graph.PortDefinition{"task": {Type: graph.ValueTypeString, Required: true}}, Config: graph.NodeConfig{"adapter": "test-adapter", "workspace": workspace, "allowed_paths": []any{"src/"}, "context_refs": []any{"docs/spec.md"}, "environment_refs": []any{"TOOL_HOME"}}, Outputs: map[string]graph.PortDefinition{"summary": {Type: graph.ValueTypeString, Required: true}, "changed_files": {Type: graph.ValueTypeJSON}, "artifacts": {Type: graph.ValueTypeJSON}, "raw_metadata": {Type: graph.ValueTypeJSON}, "usage": {Type: graph.ValueTypeJSON}}},
	}, Edges: []graph.EdgeDefinition{}, GraphInputBindings: []graph.GraphInputBindingDefinition{{From: "inputs.job", To: "work.inputs.task"}}, Outputs: map[string]graph.OutputDefinition{"result": {From: "work.outputs.summary"}}}
}

func TestAgentRuntimeDispatchAndCorrelation(t *testing.T) {
	workspace := t.TempDir()
	zero := int64(0)
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "done", ChangedFiles: []agent.ChangedFile{{Path: "src/a.go", Provenance: agent.ProvenanceReported}}, Artifacts: []agent.ArtifactReference{{Ref: "artifact:report", Name: "report"}}, RawMetadata: json.RawMessage(`{"provider":"opaque"}`), Usage: &agent.UsageMetadata{InputTokens: &zero}}}
	result, err := runtime.Run(context.Background(), agentGraph(workspace), map[string]graph.Value{"job": graph.NewStringValue("implement")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunSucceeded {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	in, ok := m.LastInput()
	if !ok || in.Task != "implement" || in.Workspace != workspace || !reflect.DeepEqual(in.AllowedPaths, []string{"src/"}) || !reflect.DeepEqual(in.ContextRefs, []string{"docs/spec.md"}) || !reflect.DeepEqual(in.EnvironmentRefs, []string{"TOOL_HOME"}) {
		t.Fatalf("input = %+v, present=%v", in, ok)
	}
	if in.RunID != result.RunID || in.NodeRunID != result.Nodes[0].NodeRunID || in.AttemptID != "" {
		t.Fatalf("correlation = %+v, result = %+v", in, result.Nodes[0])
	}
	for _, event := range result.Events {
		if event.NodeID == "work" && (event.RunID != in.RunID || event.NodeRunID != in.NodeRunID || event.AttemptID != "") {
			t.Fatalf("event correlation = %+v", event)
		}
	}
	encoded, _ := json.Marshal(result.Events)
	for _, forbidden := range []string{"implement", "TOOL_HOME", "opaque", "artifact:report"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("lifecycle event leaked %q", forbidden)
		}
	}
	if len(result.Outputs) != 1 || result.Outputs[0].Value.Type() != graph.ValueTypeString {
		t.Fatalf("graph outputs = %+v", result.Outputs)
	}
	ports := map[string]graph.Value{}
	for _, p := range result.Nodes[0].Outputs {
		ports[p.Name] = p.Value
	}
	if len(ports) != 5 {
		t.Fatalf("node outputs = %+v", ports)
	}
	for _, name := range []string{"changed_files", "artifacts", "raw_metadata", "usage"} {
		if _, ok := ports[name].AsJSON(); !ok {
			t.Fatalf("%s not JSON", name)
		}
	}
	usage, _ := ports["usage"].AsJSON()
	if string(usage) != `{"input_tokens":0}` {
		t.Fatalf("usage = %s", usage)
	}
}

func TestAgentUnknownUsageOmitted(t *testing.T) {
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "ok"}}
	result, err := runtime.Run(context.Background(), agentGraph(t.TempDir()), map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunSucceeded {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	if len(result.Nodes[0].Outputs) != 1 || result.Nodes[0].Outputs[0].Name != "summary" {
		t.Fatalf("outputs = %+v", result.Nodes[0].Outputs)
	}
}

func TestAgentConfigRejectsBeforeExecution(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*graph.NodeDefinition)
	}{
		{"unknown adapter", func(n *graph.NodeDefinition) { n.Config["adapter"] = "missing" }},
		{"missing task", func(n *graph.NodeDefinition) { delete(n.Inputs, "task") }},
		{"both task sources", func(n *graph.NodeDefinition) { n.Config["task"] = "literal" }},
		{"wrong workspace", func(n *graph.NodeDefinition) { n.Config["workspace"] = "relative" }},
		{"wrong allowed paths", func(n *graph.NodeDefinition) { n.Config["allowed_paths"] = []any{3} }},
		{"wrong context refs", func(n *graph.NodeDefinition) { n.Config["context_refs"] = []any{false} }},
		{"wrong environment refs", func(n *graph.NodeDefinition) { n.Config["environment_refs"] = "ALL" }},
		{"unknown key", func(n *graph.NodeDefinition) { n.Config["mode"] = "magic" }},
		{"unsupported port", func(n *graph.NodeDefinition) { n.Outputs["secret"] = graph.PortDefinition{Type: graph.ValueTypeString} }},
		{"wrong port type", func(n *graph.NodeDefinition) { n.Outputs["usage"] = graph.PortDefinition{Type: graph.ValueTypeString} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := agentGraph(t.TempDir())
			n := def.Nodes["work"]
			tc.mutate(&n)
			def.Nodes["work"] = n
			if tc.name == "missing task" {
				def.Inputs = map[string]graph.InputDefinition{}
				def.GraphInputBindings = nil
			}
			m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}}
			graphInputs := map[string]graph.Value{"job": graph.NewStringValue("task")}
			if tc.name == "missing task" {
				graphInputs = nil
			}
			result, err := runtime.Run(context.Background(), def, graphInputs, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
			if err != nil || result.State != runtime.RunFailed {
				t.Fatalf("Run = %+v, %v", result, err)
			}
			if _, called := m.LastInput(); called {
				t.Fatal("adapter executed despite invalid config")
			}
		})
	}
}

func TestAgentDetectionAndExecutionErrors(t *testing.T) {
	want := errors.New("provider unavailable")
	for _, tc := range []struct {
		name                string
		report              agent.CapabilityReport
		detectErr, errorErr error
		kind                agent.ErrorKind
	}{
		{"unavailable", agent.CapabilityReport{Available: false}, nil, nil, agent.ErrorUnavailable},
		{"detect error", agent.CapabilityReport{}, want, nil, agent.ErrorDetection},
		{"execute error", agent.CapabilityReport{Available: true}, nil, want, agent.ErrorExecution},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: tc.report, DetectErr: tc.detectErr, ExecuteErr: tc.errorErr}
			result, err := runtime.Run(context.Background(), agentGraph(t.TempDir()), map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
			if err != nil || result.State != runtime.RunFailed || result.Failure == nil {
				t.Fatalf("Run = %+v, %v", result, err)
			}
			var agentErr *agent.ExecutionError
			if !errors.As(result.Failure.Err, &agentErr) || agentErr.Kind != tc.kind {
				t.Fatalf("failure = %+v", result.Failure)
			}
			if tc.detectErr != nil || tc.errorErr != nil {
				if !errors.Is(result.Failure.Err, want) {
					t.Fatalf("cause = %v", result.Failure.Err)
				}
			}
		})
	}
}

func TestAgentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	workspace := t.TempDir()
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, WaitForCancel: true, Started: make(chan struct{})}
	done := make(chan runtime.RunResult, 1)
	go func() {
		result, _ := runtime.Run(ctx, agentGraph(workspace), map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
		done <- result
	}()
	<-m.Started
	cancel()
	result := <-done
	if result.State != runtime.RunCancelled || result.Nodes[0].State != runtime.NodeCancelled {
		t.Fatalf("cancel = %+v", result)
	}
}

func TestAgentInvalidRawMetadataFails(t *testing.T) {
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "ok", RawMetadata: json.RawMessage(`{bad`)}}
	result, err := runtime.Run(context.Background(), agentGraph(filepath.Join(t.TempDir(), "work")), map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunFailed {
		t.Fatalf("Run = %+v, %v", result, err)
	}
}

func TestAgentUnsupportedWorkspaceCapability(t *testing.T) {
	no := false
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true, Workspace: &no}}
	result, err := runtime.Run(context.Background(), agentGraph(t.TempDir()), map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunFailed {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	var executionErr *agent.ExecutionError
	if !errors.As(result.Failure.Err, &executionErr) || executionErr.Kind != agent.ErrorUnsupported {
		t.Fatalf("failure = %+v", result.Failure)
	}
	if _, called := m.LastInput(); called {
		t.Fatal("unsupported adapter executed")
	}
}

func TestAgentLiteralTaskWithoutBinding(t *testing.T) {
	def := agentGraph(t.TempDir())
	def.Inputs = map[string]graph.InputDefinition{}
	def.GraphInputBindings = nil
	n := def.Nodes["work"]
	n.Inputs = map[string]graph.PortDefinition{}
	n.Config["task"] = "literal task"
	def.Nodes["work"] = n
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "done"}}
	result, err := runtime.Run(context.Background(), def, nil, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunSucceeded {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	in, _ := m.LastInput()
	if in.Task != "literal task" {
		t.Fatalf("task = %q", in.Task)
	}
}

func TestAgentRequiredOptionalOutputFailsWhenUnknown(t *testing.T) {
	def := agentGraph(t.TempDir())
	n := def.Nodes["work"]
	n.Outputs["usage"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
	def.Nodes["work"] = n
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "done"}}
	result, err := runtime.Run(context.Background(), def, map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunFailed || result.Failure.Kind != runtime.FailureOutputContract {
		t.Fatalf("Run = %+v, %v", result, err)
	}
}

func TestAgentConfigDiagnosticsAreDeterministic(t *testing.T) {
	def := agentGraph(t.TempDir())
	n := def.Nodes["work"]
	n.Config["z_extra"] = true
	n.Config["a_extra"] = true
	def.Nodes["work"] = n
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}}
	for range 20 {
		result, err := runtime.Run(context.Background(), def, map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
		if err != nil || result.State != runtime.RunFailed || !strings.Contains(result.Failure.Err.Error(), `"a_extra"`) {
			t.Fatalf("Run = %+v, %v", result, err)
		}
	}
}

func TestAgentErrorUsesSchedulerOwnedIdentity(t *testing.T) {
	providerCause := errors.New("start failed")
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, ExecuteErr: &agent.ExecutionError{NodeID: "wrong", AdapterID: "other", Kind: agent.ErrorStart, Cause: providerCause}}
	result, err := runtime.Run(context.Background(), agentGraph(t.TempDir()), map[string]graph.Value{"job": graph.NewStringValue("task")}, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{})
	if err != nil || result.State != runtime.RunFailed {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	var executionErr *agent.ExecutionError
	if !errors.As(result.Failure.Err, &executionErr) || executionErr.NodeID != "work" || executionErr.AdapterID != "test-adapter" || executionErr.Kind != agent.ErrorStart || !errors.Is(executionErr, providerCause) {
		t.Fatalf("failure identity/cause = %+v", result.Failure)
	}
}

func TestAgentExecutorRejectsNilContext(t *testing.T) {
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}}
	_, err := (agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}).ExecuteNode(nil, "work", agentGraph(t.TempDir()).Nodes["work"], map[string]graph.Value{"task": graph.NewStringValue("task")})
	var executionErr *agent.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != agent.ErrorInvalidInput {
		t.Fatalf("nil context error = %v", err)
	}
}
