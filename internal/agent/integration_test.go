package agent_test

import (
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/agent/adaptertest"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
)

type compositeExecutor struct {
	agents        agent.Executor
	deterministic func(context.Context, string, graph.NodeDefinition, map[string]graph.Value, run.ExecutionCorrelation) (map[string]graph.Value, error)
}

func (e compositeExecutor) ExecuteNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
	return e.ExecuteNodeWithCorrelation(ctx, id, n, in, run.ExecutionCorrelation{})
}
func (e compositeExecutor) ExecuteNodeWithCorrelation(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, ids run.ExecutionCorrelation) (map[string]graph.Value, error) {
	if n.Type == "agent" {
		return e.agents.ExecuteNodeWithCorrelation(ctx, id, n, in, ids)
	}
	return e.deterministic(ctx, id, n, in, ids)
}
func withDownstream(d *graph.Definition) {
	d.Nodes["consume"] = graph.NodeDefinition{Type: "test", Inputs: map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString, Required: true}}, Outputs: map[string]graph.PortDefinition{"result": {Type: graph.ValueTypeString, Required: true}}}
	d.Edges = []graph.EdgeDefinition{{From: "work.outputs.summary", To: "consume.inputs.text"}}
	d.Outputs = map[string]graph.OutputDefinition{"result": {From: "consume.outputs.result"}}
}
func TestAgentSummaryEdgeToDeterministicNode(t *testing.T) {
	d := agentGraph(t.TempDir())
	withDownstream(d)
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "exact accepted agent value"}}
	var consumed run.ExecutionCorrelation
	e := compositeExecutor{agents: agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, deterministic: func(_ context.Context, _ string, _ graph.NodeDefinition, in map[string]graph.Value, ids run.ExecutionCorrelation) (map[string]graph.Value, error) {
		text, ok := in["text"].AsString()
		if !ok || text != m.Output.Summary {
			return nil, errors.New("incorrect bound summary")
		}
		consumed = ids
		return map[string]graph.Value{"result": graph.NewStringValue(text)}, nil
	}}
	result, err := runtime.Run(context.Background(), d, map[string]graph.Value{"job": graph.NewStringValue("task")}, e, runtime.Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("Run=%+v %v", result, err)
	}
	text, _ := result.Outputs[0].Value.AsString()
	if text != m.Output.Summary {
		t.Fatalf("output=%q", text)
	}
	in, _ := m.LastInput()
	if consumed.RunID != in.RunID || consumed.RunID != result.RunID || consumed.NodeRunID == in.NodeRunID || consumed.AttemptID != "" {
		t.Fatalf("correlation=%+v, %+v", in, consumed)
	}
	for _, n := range result.Nodes {
		for _, event := range result.Events {
			if event.NodeID == n.NodeID && (event.RunID != result.RunID || event.NodeRunID != n.NodeRunID || event.AttemptID != "") {
				t.Fatalf("event=%+v", event)
			}
		}
	}
}

// The failure cannot return until the independent sibling has entered execution.
type barrierAdapter struct {
	agent.AgentAdapter
	peerStarted <-chan struct{}
}

func (a barrierAdapter) Execute(ctx context.Context, input agent.AgentExecutionInput) (agent.AgentExecutionOutput, error) {
	select {
	case <-a.peerStarted:
		return a.AgentAdapter.Execute(ctx, input)
	case <-ctx.Done():
		return agent.AgentExecutionOutput{}, ctx.Err()
	}
}

func TestAgentFailureRetainsIndependentSiblingAndSafeEvents(t *testing.T) {
	d := agentGraph(t.TempDir())
	withDownstream(d)
	d.Nodes["sibling"] = graph.NodeDefinition{Type: "test", Outputs: map[string]graph.PortDefinition{"kept": {Type: graph.ValueTypeString, Required: true}}}
	const sentinel = "PROVIDER_FAILURE_SENTINEL"
	cause := errors.New(sentinel)
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, ExecuteErr: cause, Started: make(chan struct{}), Output: agent.AgentExecutionOutput{RawMetadata: json.RawMessage(`{"private":"METADATA_SENTINEL"}`), Artifacts: []agent.ArtifactReference{{Ref: "ARTIFACT_SENTINEL"}}}}
	siblingStarted := make(chan struct{})
	e := compositeExecutor{agents: agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": barrierAdapter{AgentAdapter: m, peerStarted: siblingStarted}}}, deterministic: func(_ context.Context, id string, _ graph.NodeDefinition, _ map[string]graph.Value, _ run.ExecutionCorrelation) (map[string]graph.Value, error) {
		if id != "sibling" {
			return nil, errors.New("dependent must not execute")
		}
		close(siblingStarted)
		<-m.Started
		return map[string]graph.Value{"kept": graph.NewStringValue("immutable accepted sibling")}, nil
	}}
	result, err := runtime.Run(context.Background(), d, map[string]graph.Value{"job": graph.NewStringValue("TASK_SENTINEL")}, e, runtime.Options{GraphVersion: yamlgraph.Marshal, MaxConcurrency: 2})
	<-siblingStarted
	if err != nil || result.State != run.RunFailed || !errors.Is(result.Failure.Err, cause) {
		t.Fatalf("Run=%+v %v", result, err)
	}
	for _, n := range result.Nodes {
		switch n.NodeID {
		case "consume":
			if n.State != run.NodeSkipped {
				t.Fatalf("dependent=%+v", n)
			}
		case "work":
			if n.State != run.NodeFailed || len(n.Outputs) != 0 {
				t.Fatalf("failed=%+v", n)
			}
		case "sibling":
			if n.State != run.NodeSucceeded || len(n.Outputs) != 1 {
				t.Fatalf("sibling=%+v", n)
			}
			value, _ := n.Outputs[0].Value.AsString()
			if value != "immutable accepted sibling" {
				t.Fatalf("sibling value=%q", value)
			}
		}
	}
	data, err := json.Marshal(result.Events)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{sentinel, "TASK_SENTINEL", "METADATA_SENTINEL", "ARTIFACT_SENTINEL", "TOOL_HOME", "docs/spec.md"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("event leaked %q", secret)
		}
	}
}

func TestAgentYAMLRoundTripExecutionAndSemanticVersion(t *testing.T) {
	// An absolute path on every OS (/tmp/agent-workspace on Linux CI).
	workspace := filepath.Join(os.TempDir(), "agent-workspace")
	source := `schema_version: 0
id: parsed-agent
inputs: {}
nodes:
  work:
    type: agent
    config:
      adapter: test-adapter
      task: explicit task
      workspace: ` + strconv.Quote(workspace) + `
      allowed_paths: [src/]
      context_refs: [docs/spec.md]
      environment_refs: [TOOL_HOME]
    outputs:
      summary: {type: string, required: true}
edges: []
outputs:
  result: {from: work.outputs.summary}
`
	d, err := yamlgraph.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := yamlgraph.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := yamlgraph.Parse(encoded)
	if err != nil || !reflect.DeepEqual(d, decoded) {
		t.Fatalf("round trip=%+v %v", decoded, err)
	}
	base, err := yamlgraph.SemanticVersion(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"adapter", "task", "workspace", "allowed_paths", "context_refs", "environment_refs"} {
		clone, _ := yamlgraph.Parse(encoded)
		if strings.HasSuffix(field, "refs") || field == "allowed_paths" {
			clone.Nodes["work"].Config[field] = []any{"changed"}
		} else {
			clone.Nodes["work"].Config[field] = "changed"
		}
		hash, err := yamlgraph.SemanticVersion(clone)
		if err != nil || hash == base {
			t.Fatalf("%s hash=%q %v", field, hash, err)
		}
	}
	clone, _ := yamlgraph.Parse(encoded)
	clone.Metadata = map[string]any{"layout": "changed"}
	hash, err := yamlgraph.SemanticVersion(clone)
	if err != nil || hash != base {
		t.Fatalf("metadata hash=%s %v", hash, err)
	}
	m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: agent.AgentExecutionOutput{Summary: "parsed fixture done"}}
	result, err := runtime.Run(context.Background(), decoded, nil, agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}, runtime.Options{GraphVersion: yamlgraph.Marshal})
	if err != nil || result.State != run.RunSucceeded {
		t.Fatalf("parsed Run=%+v %v", result, err)
	}
	input, _ := m.LastInput()
	if input.Task != "explicit task" || input.Workspace != workspace || !reflect.DeepEqual(input.AllowedPaths, []string{"src/"}) || !reflect.DeepEqual(input.ContextRefs, []string{"docs/spec.md"}) || !reflect.DeepEqual(input.EnvironmentRefs, []string{"TOOL_HOME"}) {
		t.Fatalf("parsed input=%+v", input)
	}
}
