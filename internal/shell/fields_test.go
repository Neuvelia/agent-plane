package shell

import (
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
	"agentplane/internal/fields/fieldstest"
)

func shellBase() graph.NodeDefinition {
	return graph.NodeDefinition{
		Type:   "shell",
		Config: graph.NodeConfig{"command": "/bin/echo"},
		Inputs: map[string]graph.PortDefinition{"name": {Type: graph.ValueTypeString, Required: true}},
	}
}

func TestConfigFieldsEqualTheExecutor(t *testing.T) {
	fieldstest.Run(t, fieldstest.Spec{
		Fields: ConfigFields(),
		Base:   shellBase(),
		Samples: map[string]any{
			"shell.config.command":       "/bin/echo",
			"shell.config.args":          []any{"a", "b"},
			"shell.config.cwd":           t.TempDir(),
			"shell.config.env_allowlist": []any{"HOME"},
			"shell.config.env":           map[string]any{"MODE": "x"},
			"shell.config.input_env":     map[string]any{"name": "NAME_VAR"},
			"shell.config.secret_env":    map[string]any{"TOKEN": "gh_token"},
			"shell.config.stdout_json":   true,
		},
		Validate: func(n graph.NodeDefinition) error { return Executor{}.ValidateNodeConfig("n", n) },
	})
}

func TestOutputPortsEqualTheExecutor(t *testing.T) {
	fieldstest.RunPorts(t, fieldstest.PortSpec{
		Ports: OutputPorts(),
		Base:  shellBase(),
		Satisfy: func(n *graph.NodeDefinition, r fields.Rule) {
			n.Config[r.Key] = r.Equals == "true"
		},
		// A Shell node produces text and an exit code; it does not check a
		// declared port's type statically (graph bindings check edges).
		TypeChecked: false,
		Validate: func(n graph.NodeDefinition) error {
			cfg, err := parseConfig(n)
			if err != nil {
				return err
			}
			_, err = checkOutputs(n, cfg)
			return err
		},
	})
}

func TestConfigKeysArePinnedToTheSpec(t *testing.T) {
	fieldstest.PinKeys(t, ConfigFields(), "command", "args", "cwd", "env_allowlist", "env", "input_env", "secret_env", "stdout_json")
}
