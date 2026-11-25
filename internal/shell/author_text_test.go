package shell

import (
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

// Both forms fail the environment identifier grammar; the second still passes
// the looser environment-name check, so it reaches the conflict rules.
const (
	pastedAssignment = "TOKEN=sk-Live9f3Q"
	pastedValue      = "sk-Live9f3Q"
)

func TestConfigErrorsNeverQuoteAuthorText(t *testing.T) {
	inputs := map[string]graph.PortDefinition{"text": {Type: graph.ValueTypeString}}
	for _, tc := range []struct {
		name   string
		id     string
		node   graph.NodeDefinition
		inform string
	}{
		{"node id", pastedAssignment, shellNode(graph.NodeConfig{"cwd": 1}, nil), "shell node <invalid identifier>"},
		{"node type", "a", graph.NodeDefinition{Type: pastedAssignment}, "node type"},
		{"unknown field", "a", shellNode(graph.NodeConfig{pastedAssignment: "x"}, nil), "unknown shell config field"},
		{"env_allowlist name", "a", shellNode(graph.NodeConfig{"env_allowlist": []string{"PATH", pastedAssignment}}, nil), "env_allowlist[1]"},
		{"env name", "a", shellNode(graph.NodeConfig{"env": map[string]string{pastedAssignment: "x"}}, nil), "env entry 0"},
		{"env value type", "a", shellNode(graph.NodeConfig{"env": map[string]any{pastedValue: 1}}, nil), "env entry 0 must be a string"},
		{"env NUL", "a", shellNode(graph.NodeConfig{"env": map[string]string{pastedValue: "a\x00"}}, nil), "env entry 0 contains a NUL byte"},
		{"input_env undeclared input", "a", shellNode(graph.NodeConfig{"input_env": map[string]string{pastedValue: "X"}}, nil), "undeclared input"},
		{"input_env name", "a", shellNode(graph.NodeConfig{"input_env": map[string]string{"text": pastedAssignment}}, inputs), `input_env input "text" has an invalid environment name`},
		{"input_env conflicts with env", "a", shellNode(graph.NodeConfig{"env": map[string]string{pastedValue: "x"}, "input_env": map[string]string{"text": pastedValue}}, inputs), `input "text" maps to environment name <not shown>, which conflicts with env`},
		{"input_env conflicts with env_allowlist", "a", shellNode(graph.NodeConfig{"env_allowlist": []string{pastedValue}, "input_env": map[string]string{"text": pastedValue}}, inputs), "conflicts with env_allowlist"},
		{"secret_env undeclared secret name", "a", shellNode(graph.NodeConfig{"secret_env": map[string]string{pastedValue: "Bad Name"}}, nil), "secret_env entry 0 must name a declared secret"},
		{"secret_env conflict", "a", shellNode(graph.NodeConfig{"env": map[string]string{pastedValue: "x"}, "secret_env": map[string]string{pastedValue: "token"}}, nil), "secret_env entry 0 conflicts with env"},
		{"secret_env duplicate", "a", shellNode(graph.NodeConfig{"secret_env": map[string]string{"sk-live9f3q": "a", pastedValue: "b"}}, nil), "secret_env entry 1 repeats an earlier environment name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (Executor{}).ValidateNodeConfig(tc.id, tc.node)
			if err == nil {
				t.Fatal("expected a configuration error")
			}
			if strings.Contains(strings.ToLower(err.Error()), "live9f3q") {
				t.Fatalf("error echoes author text: %v", err)
			}
			if !strings.Contains(err.Error(), tc.inform) {
				t.Fatalf("error %q does not say %q", err, tc.inform)
			}
		})
	}
}

func TestConfigErrorsStillQuoteNamesThatMatchTheGrammar(t *testing.T) {
	node := shellNode(graph.NodeConfig{"env": map[string]string{"GH_TOKEN": "x"}, "secret_env": map[string]string{"GH_TOKEN": "gh"}}, nil)
	err := (Executor{}).ValidateNodeConfig("build", node)
	if err == nil || !strings.Contains(err.Error(), `shell node "build": secret_env["GH_TOKEN"] conflicts with env`) {
		t.Fatalf("error = %v", err)
	}
}

func shellNode(config graph.NodeConfig, inputs map[string]graph.PortDefinition) graph.NodeDefinition {
	merged := graph.NodeConfig{"command": "/bin/true"}
	for key, value := range config {
		merged[key] = value
	}
	return graph.NodeDefinition{Type: "shell", Config: merged, Inputs: inputs}
}
