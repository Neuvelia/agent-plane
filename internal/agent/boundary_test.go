package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"agentplane/internal/agent"
	"agentplane/internal/agent/adaptertest"
	"agentplane/internal/domain/graph"
)

func TestAgentExecutorConfigBoundary(t *testing.T) {
	cases := map[string]func(*graph.NodeDefinition, map[string]graph.Value){
		"wrong node type":     func(n *graph.NodeDefinition, _ map[string]graph.Value) { n.Type = "shell" },
		"missing task":        func(n *graph.NodeDefinition, _ map[string]graph.Value) { delete(n.Inputs, "task") },
		"both task sources":   func(n *graph.NodeDefinition, _ map[string]graph.Value) { n.Config["task"] = "duplicate" },
		"unresolved task":     func(_ *graph.NodeDefinition, v map[string]graph.Value) { delete(v, "task") },
		"empty resolved task": func(_ *graph.NodeDefinition, v map[string]graph.Value) { v["task"] = graph.NewStringValue(" ") },
		"wrong resolved task": func(_ *graph.NodeDefinition, v map[string]graph.Value) {
			v["task"], _ = graph.NewJSONValue([]byte(`1`))
		},
		"wrong task port": func(n *graph.NodeDefinition, _ map[string]graph.Value) {
			n.Inputs["task"] = graph.PortDefinition{Type: graph.ValueTypeJSON}
		},
		"unknown key": func(n *graph.NodeDefinition, _ map[string]graph.Value) { n.Config["hidden"] = true },
	}
	for _, field := range []string{"adapter", "workspace", "task"} {
		for _, value := range []any{nil, "", " ", 42} {
			field, value := field, value
			cases[field+"/"+jsonLabel(value)] = func(n *graph.NodeDefinition, _ map[string]graph.Value) {
				if field == "task" {
					delete(n.Inputs, "task")
				}
				if value == nil {
					delete(n.Config, field)
				} else {
					n.Config[field] = value
				}
			}
		}
	}
	for _, field := range []string{"allowed_paths", "context_refs", "environment_refs"} {
		for _, value := range []any{nil, "all", []any{false}, []any{""}, []string{" "}} {
			field, value := field, value
			cases[field+"/"+jsonLabel(value)] = func(n *graph.NodeDefinition, _ map[string]graph.Value) { n.Config[field] = value }
		}
	}
	for _, port := range []string{"summary", "changed_files", "artifacts", "raw_metadata", "usage", "unsupported"} {
		port := port
		cases["port/"+port] = func(n *graph.NodeDefinition, _ map[string]graph.Value) {
			typ := graph.ValueTypeString
			if port == "summary" {
				typ = graph.ValueTypeJSON
			}
			n.Outputs[port] = graph.PortDefinition{Type: typ}
		}
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			n := agentGraph(t.TempDir()).Nodes["work"]
			values := map[string]graph.Value{"task": graph.NewStringValue("task")}
			mutate(&n, values)
			m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}}
			outputs, err := (agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}).ExecuteNode(context.Background(), "work", n, values)
			var typed *agent.ExecutionError
			if outputs != nil || !errors.As(err, &typed) || typed.Kind != agent.ErrorInvalidInput || typed.NodeID != "work" {
				t.Fatalf("outputs=%v, error=%v", outputs, err)
			}
			if _, called := m.LastInput(); called {
				t.Fatal("invalid config executed")
			}
		})
	}
}
func jsonLabel(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestAgentRegistryBoundary(t *testing.T) {
	for _, registry := range []map[agent.AdapterID]agent.AgentAdapter{
		nil, {"test-adapter": nil}, {"test-adapter": &adaptertest.MockAdapter{AdapterID: "other"}},
	} {
		_, err := (agent.Executor{Adapters: registry}).ExecuteNode(context.Background(), "work", agentGraph(t.TempDir()).Nodes["work"], map[string]graph.Value{"task": graph.NewStringValue("task")})
		var typed *agent.ExecutionError
		if !errors.As(err, &typed) || typed.Kind != agent.ErrorUnavailable || typed.NodeID != "work" || typed.AdapterID != "test-adapter" {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestAgentOutputBoundary(t *testing.T) {
	for name, output := range map[string]agent.AgentExecutionOutput{
		"raw JSON":           {RawMetadata: json.RawMessage(`{broken`)},
		"missing path":       {ChangedFiles: []agent.ChangedFile{{Provenance: agent.ProvenanceReported}}},
		"missing provenance": {ChangedFiles: []agent.ChangedFile{{Path: "a.go"}}},
		"unknown provenance": {ChangedFiles: []agent.ChangedFile{{Path: "a.go", Provenance: "invented"}}},
		"artifact":           {Artifacts: []agent.ArtifactReference{{Name: "missing ref"}}},
	} {
		t.Run(name, func(t *testing.T) {
			output.Summary = "must not be accepted"
			m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: output}
			n := agentGraph(t.TempDir()).Nodes["work"]
			outputs, err := (agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}).ExecuteNode(context.Background(), "work", n, map[string]graph.Value{"task": graph.NewStringValue("task")})
			var typed *agent.ExecutionError
			if outputs != nil || !errors.As(err, &typed) || typed.Kind != agent.ErrorExecution || typed.AdapterID != "test-adapter" || typed.NodeID != "work" {
				t.Fatalf("outputs=%v error=%v", outputs, err)
			}
		})
	}
}

func TestAgentAbsentEmptyAndUndeclaredOutputs(t *testing.T) {
	for _, present := range []bool{false, true} {
		output := agent.AgentExecutionOutput{Summary: "ok"}
		if present {
			output.ChangedFiles = []agent.ChangedFile{}
			output.Artifacts = []agent.ArtifactReference{}
			output.Usage = &agent.UsageMetadata{}
		}
		m := &adaptertest.MockAdapter{AdapterID: "test-adapter", Report: agent.CapabilityReport{Available: true}, Output: output}
		n := agentGraph(t.TempDir()).Nodes["work"]
		e := agent.Executor{Adapters: map[agent.AdapterID]agent.AgentAdapter{"test-adapter": m}}
		outputs, err := e.ExecuteNode(context.Background(), "work", n, map[string]graph.Value{"task": graph.NewStringValue("task")})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"changed_files", "artifacts", "usage"} {
			value, ok := outputs[name]
			if ok != present {
				t.Fatalf("%s present=%v", name, ok)
			}
			if present {
				data, _ := value.AsJSON()
				want := "[]"
				if name == "usage" {
					want = "{}"
				}
				if string(data) != want {
					t.Fatalf("%s=%s", name, data)
				}
			}
		}
		n.Outputs = map[string]graph.PortDefinition{}
		outputs, err = e.ExecuteNode(context.Background(), "work", n, map[string]graph.Value{"task": graph.NewStringValue("task")})
		if err != nil || len(outputs) != 0 {
			t.Fatalf("undeclared=%v, %v", outputs, err)
		}
	}
}

func TestAgentInvalidConfigRetainsKnownAdapterIdentity(t *testing.T) {
	n := agentGraph(t.TempDir()).Nodes["work"]
	n.Config["workspace"] = "relative"
	_, err := (agent.Executor{}).ExecuteNode(context.Background(), "work", n, map[string]graph.Value{"task": graph.NewStringValue("task")})
	var typed *agent.ExecutionError
	if !errors.As(err, &typed) || typed.NodeID != "work" || typed.AdapterID != "test-adapter" || typed.Kind != agent.ErrorInvalidInput {
		t.Fatalf("error identity=%+v", typed)
	}
}
