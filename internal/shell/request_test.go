package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

const requestCanary = "ghp_Request0Canary5Value"

func grant(variable, value string) secrets.Env {
	return secrets.NewEnv([]secrets.Binding{{Variable: variable, Value: secrets.NewValue(value)}})
}

func envLenNode(secretEnv map[string]any, names ...string) graph.NodeDefinition {
	node := helperNode("env-len", names...)
	node.Config["secret_env"] = secretEnv
	node.Outputs = map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString, Required: true}}
	return node
}

func stdoutOf(t *testing.T, outputs map[string]graph.Value) string {
	t.Helper()
	text, ok := outputs["stdout"].AsString()
	if !ok {
		t.Fatalf("stdout is not a string: %v", outputs)
	}
	return text
}

func TestRunDeliversSecretEnvToChild(t *testing.T) {
	node := envLenNode(map[string]any{"GH_TOKEN": "gh_token"}, "GH_TOKEN", "OTHER")
	outputs, err := Executor{}.Run(context.Background(), Request{NodeID: "n", Node: node, Secrets: grant("GH_TOKEN", requestCanary)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdoutOf(t, outputs), fmt.Sprintf("GH_TOKEN=%d;OTHER=-;", len(requestCanary)); got != want {
		t.Fatalf("child saw %q, want %q", got, want)
	}
}

func TestRunWithoutGrantStartsNoProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	node := helperNode("touch", marker)
	node.Config["secret_env"] = map[string]any{"GH_TOKEN": "gh_token"}
	grants := map[string]secrets.Env{
		"none":      {},
		"partial":   grant("OTHER_TOKEN", requestCanary),
		"different": grant("gh_token_lowercase", requestCanary),
	}
	for name, env := range grants {
		t.Run(name, func(t *testing.T) {
			_, err := Executor{}.Run(context.Background(), Request{NodeID: "n", Node: node, Secrets: env})
			var executionErr *ExecutionError
			if !errors.As(err, &executionErr) || executionErr.Kind != FailureConfig || !errors.Is(err, errSecretNotGranted) {
				t.Fatalf("error = %v, want a FailureConfig wrapping errSecretNotGranted", err)
			}
			if strings.Contains(err.Error(), "GH_TOKEN") || strings.Contains(err.Error(), "gh_token") || strings.Contains(err.Error(), requestCanary) {
				t.Fatalf("error names a variable or secret: %v", err)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("process ran: %v", statErr)
			}
		})
	}
}

func TestRunRequiresGrantForEveryConfiguredVariable(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	node := helperNode("touch", marker)
	node.Config["secret_env"] = map[string]any{"A_TOKEN": "a", "B_TOKEN": "b"}
	_, err := Executor{}.Run(context.Background(), Request{NodeID: "n", Node: node, Secrets: grant("A_TOKEN", requestCanary)})
	if !errors.Is(err, errSecretNotGranted) {
		t.Fatalf("error = %v, want errSecretNotGranted", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("process ran: %v", statErr)
	}
}

func TestRunIgnoresGrantedVariableTheNodeDoesNotConfigure(t *testing.T) {
	node := envLenNode(map[string]any{"GH_TOKEN": "gh_token"}, "GH_TOKEN", "EXTRA")
	both := secrets.NewEnv([]secrets.Binding{
		{Variable: "GH_TOKEN", Value: secrets.NewValue(requestCanary)},
		{Variable: "EXTRA", Value: secrets.NewValue(requestCanary)},
	})
	outputs, err := Executor{}.Run(context.Background(), Request{NodeID: "n", Node: node, Secrets: both})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdoutOf(t, outputs), fmt.Sprintf("GH_TOKEN=%d;EXTRA=-;", len(requestCanary)); got != want {
		t.Fatalf("child saw %q, want %q", got, want)
	}
}

func TestRunSecretEnvCoexistsWithAttemptVariable(t *testing.T) {
	node := envLenNode(map[string]any{"GH_TOKEN": "gh_token"}, "GH_TOKEN", AttemptEnv)
	outputs, err := Executor{}.Run(context.Background(), Request{NodeID: "n", Node: node, Attempt: 2, Secrets: grant("GH_TOKEN", requestCanary)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stdoutOf(t, outputs), fmt.Sprintf("GH_TOKEN=%d;%s=1;", len(requestCanary), AttemptEnv); got != want {
		t.Fatalf("child saw %q, want %q", got, want)
	}
}

func TestRunSecretEnvDoesNotLeakIntoFailure(t *testing.T) {
	node := helperNode("fail")
	node.Config["secret_env"] = map[string]any{"GH_TOKEN": "gh_token"}
	request := Request{NodeID: "n", Node: node, Secrets: grant("GH_TOKEN", requestCanary)}
	_, err := Executor{}.Run(context.Background(), request)
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureExit {
		t.Fatalf("error = %v, want an exit failure", err)
	}
	if text := err.Error() + fmt.Sprintf("%+v %#v", executionErr.Result, request); strings.Contains(text, requestCanary) {
		t.Fatalf("failure or request print holds the secret: %s", text)
	}
}

func TestExecuteNodeAttemptWithoutSecretsStillRuns(t *testing.T) {
	node := helperNode("echo", "x")
	node.Outputs = map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString, Required: true}}
	outputs, err := Executor{}.ExecuteNodeAttempt(context.Background(), "n", node, nil, nil, 0)
	if err != nil || stdoutOf(t, outputs) != `["x"]` {
		t.Fatalf("outputs = %v, err = %v", outputs, err)
	}
}
