package gate

import (
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
	"agentplane/internal/fields/fieldstest"
)

func required(t graph.ValueType) graph.PortDefinition {
	return graph.PortDefinition{Type: t, Required: true}
}

// gateBases returns a valid Gate node of each kind.
func gateBases(dir string) map[string]graph.NodeDefinition {
	node := func(config graph.NodeConfig, inputs map[string]graph.PortDefinition) graph.NodeDefinition {
		config["check_id"] = "check"
		return graph.NodeDefinition{Type: "gate", Config: config, Inputs: inputs}
	}
	return map[string]graph.NodeDefinition{
		KindCommand: node(graph.NodeConfig{"kind": KindCommand, "command": diffCommand, "cwd": dir, "left_input": "left", "right_input": "right"},
			map[string]graph.PortDefinition{"left": required(graph.ValueTypeString), "right": required(graph.ValueTypeString)}),
		KindJSONPredicate: node(graph.NodeConfig{"kind": KindJSONPredicate, "input": "doc", "field": []any{"status"}, "operator": "eq", "expected": "ok"},
			map[string]graph.PortDefinition{"doc": required(graph.ValueTypeJSON)}),
		KindReportedFileScope: node(graph.NodeConfig{"kind": KindReportedFileScope, "input": "files", "allowed_paths": []any{"src/"}},
			map[string]graph.PortDefinition{"files": required(graph.ValueTypeJSON)}),
		KindChangedFilesScope: node(graph.NodeConfig{"kind": KindChangedFilesScope, "input": "candidate", "allowed_paths": []any{"src/"}, "mode": "warn", "workspace": dir},
			map[string]graph.PortDefinition{"candidate": required(graph.ValueTypeString)}),
	}
}

func TestConfigFieldsEqualTheExecutorForEveryKind(t *testing.T) {
	dir := t.TempDir()
	samples := map[string]any{
		"gate.config.check_id":                          "check",
		"gate.config.command.command":                   diffCommand,
		"gate.config.command.cwd":                       dir,
		"gate.config.command.left_input":                "left",
		"gate.config.command.right_input":               "right",
		"gate.config.json_predicate.input":              "doc",
		"gate.config.json_predicate.field":              []any{"result", "status"},
		"gate.config.json_predicate.operator":           "ne",
		"gate.config.json_predicate.expected":           "ok",
		"gate.config.json_predicate.expected_json":      `"ok"`,
		"gate.config.reported_file_scope.input":         "files",
		"gate.config.reported_file_scope.allowed_paths": []any{"src/", "docs/a.md"},
		"gate.config.changed_files_scope.input":         "candidate",
		"gate.config.changed_files_scope.allowed_paths": []any{"src/", "docs/a.md"},
		"gate.config.changed_files_scope.mode":          "deny",
		"gate.config.changed_files_scope.workspace":     dir,
	}
	kinds := fields.AllowedKeys(ConfigFields(), nil)
	if !kinds["kind"] || !kinds["check_id"] {
		t.Fatal("check_id and kind must apply to every Gate")
	}
	for kind, base := range gateBases(dir) {
		t.Run(kind, func(t *testing.T) {
			s := map[string]any{"gate.config.kind": kind}
			for k, v := range samples {
				s[k] = v
			}
			fieldstest.Run(t, fieldstest.Spec{
				Fields:   ConfigFields(),
				Base:     base,
				Samples:  s,
				Validate: func(n graph.NodeDefinition) error { return Executor{}.ValidateNodeConfig("n", n) },
			})
		})
	}
}

func TestKindEnumListsEveryKindTheExecutorAccepts(t *testing.T) {
	bases := gateBases(t.TempDir())
	enum := ConfigFields()[1].Enum
	if len(enum) != len(bases) {
		t.Fatalf("kind enum %v does not match the %d kinds with a valid base", enum, len(bases))
	}
	for _, k := range enum {
		if _, ok := bases[k]; !ok {
			t.Errorf("kind %q has no valid base node", k)
		}
	}
}

func TestOutputPortsEqualTheExecutor(t *testing.T) {
	fieldstest.RunPorts(t, fieldstest.PortSpec{
		Ports:       OutputPorts(),
		Base:        gateBases(t.TempDir())[KindJSONPredicate],
		Satisfy:     func(*graph.NodeDefinition, fields.Rule) {},
		TypeChecked: true,
		Validate:    func(n graph.NodeDefinition) error { return Executor{}.ValidateNodeConfig("n", n) },
	})
}

func TestConfigKeysArePinnedToTheSpec(t *testing.T) {
	fieldstest.PinKeys(t, ConfigFields(), "check_id", "kind", "command", "cwd", "left_input", "right_input",
		"input", "field", "operator", "expected", "expected_json", "allowed_paths", "mode", "workspace")
}
