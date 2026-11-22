package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
)

func secretEnvNode(secretEnv any, more graph.NodeConfig) graph.NodeDefinition {
	node := graph.NodeDefinition{Type: "shell", Config: graph.NodeConfig{"command": "/bin/echo", "secret_env": secretEnv}}
	node.Inputs = map[string]graph.PortDefinition{"name": {Type: graph.ValueTypeString, Required: true}}
	for key, value := range more {
		node.Config[key] = value
	}
	return node
}

func TestShellConfigAcceptsSecretEnv(t *testing.T) {
	node := secretEnvNode(map[string]any{"GH_TOKEN": "gh_token"}, nil)
	if err := (Executor{}).ValidateNodeConfig("n", node); err != nil {
		t.Fatalf("ValidateNodeConfig() = %v", err)
	}
}

func TestShellConfigRejectsInvalidSecretEnv(t *testing.T) {
	const pasted = "ghp_PastedSecretValue"
	tests := []struct {
		name  string
		value any
		more  graph.NodeConfig
	}{
		{"not a mapping", []any{"tok"}, nil},
		{"value not a string", map[string]any{"T": 1}, nil},
		{"empty variable name", map[string]any{"": "tok"}, nil},
		{"variable name with equals", map[string]any{"A=B": "tok"}, nil},
		{"variable name with NUL", map[string]any{"A\x00B": "tok"}, nil},
		{"reserved variable", map[string]any{"agentplane_attempt": "tok"}, nil},
		{"secret name uppercase", map[string]any{"T": "Bad-Name"}, nil},
		{"secret name empty", map[string]any{"T": ""}, nil},
		{"pasted secret as name", map[string]any{"T": pasted}, nil},
		{"duplicate variable", map[string]any{"T": "a", "t": "b"}, nil},
		{"collides with env", map[string]any{"T": "a"}, graph.NodeConfig{"env": map[string]any{"t": "x"}}},
		{"collides with env_allowlist", map[string]any{"T": "a"}, graph.NodeConfig{"env_allowlist": []any{"t"}}},
		{"collides with input_env", map[string]any{"T": "a"}, graph.NodeConfig{"input_env": map[string]any{"name": "t"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Executor{}.ValidateNodeConfig("n", secretEnvNode(test.value, test.more))
			if err == nil {
				t.Fatal("ValidateNodeConfig() = nil, want an error")
			}
			if strings.Contains(err.Error(), pasted) {
				t.Fatalf("error quotes the secret name text: %v", err)
			}
		})
	}
}

func TestShellConfigDoesNotQuoteInvalidSecretEnvNames(t *testing.T) {
	tests := []struct{ name, variable, leaked string }{
		{"pasted assignment", "GH_TOKEN=ghp_xxx", "ghp_xxx"},
		{"empty name", "", `""`},
		{"NUL in name", "a\x00b", "a\x00b"},
		{"NUL escaped", "a\x00b", `\x00`},
		{"reserved name", "agentplane_attempt", "agentplane_attempt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Executor{}.ValidateNodeConfig("n", secretEnvNode(map[string]any{test.variable: "tok"}, nil))
			if err == nil {
				t.Fatal("ValidateNodeConfig() = nil, want an error")
			}
			if !strings.Contains(err.Error(), "secret_env entry 0 has an invalid environment name") || strings.Contains(err.Error(), test.leaked) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestShellConfigKeepsQuotingValidSecretEnvNames(t *testing.T) {
	err := Executor{}.ValidateNodeConfig("n", secretEnvNode(map[string]any{"GH_TOKEN": 1}, nil))
	if err == nil || !strings.Contains(err.Error(), `secret_env["GH_TOKEN"] must be a string`) {
		t.Fatalf("error = %v", err)
	}
}

func TestExecuteRefusesSecretEnvWithoutDeliveryAndNeverStartsTheProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	node := helperNode("touch", marker)
	node.Config["secret_env"] = map[string]any{"GH_TOKEN": "gh_token"}
	calls := map[string]func() (Result, error){
		"Execute": func() (Result, error) { return Executor{}.Execute(context.Background(), "n", node, nil) },
		"attempt": func() (Result, error) {
			_, err := Executor{}.ExecuteNodeAttempt(context.Background(), "n", node, nil, nil, 1)
			return Result{ExitCode: -1}, err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			result, err := call()
			var executionErr *ExecutionError
			if !errors.As(err, &executionErr) || executionErr.Kind != FailureConfig || !errors.Is(err, errSecretDeliveryUnavailable) {
				t.Fatalf("error = %v", err)
			}
			if result.ExitCode != -1 {
				t.Fatalf("ExitCode = %d, want -1", result.ExitCode)
			}
			if strings.Contains(err.Error(), "GH_TOKEN") || strings.Contains(err.Error(), "gh_token") {
				t.Fatalf("error names the secret: %v", err)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("process ran: %v", statErr)
			}
		})
	}
}

func TestSecretEnvNeverEntersTheChildEnvironment(t *testing.T) {
	cfg, err := parseConfig(secretEnvNode(map[string]any{"GH_TOKEN": "gh_token"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range buildEnvironment(cfg) {
		if strings.Contains(entry, "GH_TOKEN") || strings.Contains(entry, "gh_token") {
			t.Fatalf("environment holds %q", entry)
		}
	}
}
