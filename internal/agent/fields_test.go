package agent

import (
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
	"agentplane/internal/fields/fieldstest"
)

func agentBase(workspace string) graph.NodeDefinition {
	return graph.NodeDefinition{
		Type:   "agent",
		Config: graph.NodeConfig{"adapter": "codex", "workspace": workspace, "task": "fix the bug"},
	}
}

func TestConfigFieldsEqualTheExecutor(t *testing.T) {
	ws := t.TempDir()
	fieldstest.Run(t, fieldstest.Spec{
		Fields: ConfigFields(),
		Base:   agentBase(ws),
		Samples: map[string]any{
			"agent.config.adapter":          "codex",
			"agent.config.workspace":        ws,
			"agent.config.task":             "fix the bug",
			"agent.config.allowed_paths":    []any{"src", "docs/a.md"},
			"agent.config.context_refs":     []any{"docs/spec.md"},
			"agent.config.environment_refs": []any{"API_TOKEN"},
		},
		Validate: func(n graph.NodeDefinition) error { return Executor{}.ValidateNodeConfig("n", n) },
	})
}

func TestTaskFromAStringInputEqualsTheExecutor(t *testing.T) {
	n := agentBase(t.TempDir())
	delete(n.Config, "task")
	n.Inputs = map[string]graph.PortDefinition{"task": {Type: graph.ValueTypeString, Required: true}}
	if err := (Executor{}).ValidateNodeConfig("n", n); err != nil {
		t.Fatalf("a task input instead of config.task is refused: %v", err)
	}
	n.Inputs["task"] = graph.PortDefinition{Type: graph.ValueTypeJSON, Required: true}
	if (Executor{}).ValidateNodeConfig("n", n) == nil {
		t.Fatal("a non-string task input is accepted")
	}
}

func TestOutputPortsEqualTheExecutor(t *testing.T) {
	fieldstest.RunPorts(t, fieldstest.PortSpec{
		Ports:       OutputPorts(),
		Base:        agentBase(t.TempDir()),
		Satisfy:     func(*graph.NodeDefinition, fields.Rule) {},
		TypeChecked: true,
		Validate:    func(n graph.NodeDefinition) error { return Executor{}.ValidateNodeConfig("n", n) },
	})
}

func TestConfigKeysArePinnedToTheSpec(t *testing.T) {
	fieldstest.PinKeys(t, ConfigFields(), "adapter", "workspace", "task", "allowed_paths", "context_refs", "environment_refs")
}

func TestTaskDescriptorStatesItsAlternative(t *testing.T) {
	for _, f := range ConfigFields() {
		if f.Key == "task" {
			if !f.Required || len(f.Excludes) != 1 || f.Excludes[0] != (fields.Rule{Input: "task"}) {
				t.Fatalf("task must be required with the task input as its one alternative, got %+v", f)
			}
			return
		}
	}
	t.Fatal("no task descriptor")
}
