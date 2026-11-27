package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
	"agentplane/internal/testenv"
)

// failedSecretEchoRun runs a Shell node that echoes its granted secret to both
// streams and exits 3, so the failure carries shell.ExecutionError evidence.
func failedSecretEchoRun(t *testing.T) run.Result {
	t.Helper()
	testenv.RequireExecutable(t, "/bin/sh")
	t.Setenv(deliveryEnvName, deliveryValue)
	node := "  a:\n    type: shell\n    config: {command: /bin/sh, args: [-c, 'echo \"$T\"; echo \"$T\" >&2; exit 3'], secret_env: {T: tok}}\n"
	def := parseGraphText(t, secretGraphText(node))
	options := runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, SecretResolver: secretResolver()}
	result, err := runtime.Run(context.Background(), def, nil, selectExecutor(def, cliAgentRemedy), options)
	if err != nil || result.State != run.RunFailed || result.Failure == nil {
		t.Fatalf("run = %v %v", result.State, err)
	}
	return result
}

func TestFailureErrorsAsDoesNotExposeRawShellEvidence(t *testing.T) {
	result := failedSecretEchoRun(t)
	if strings.Contains(result.Failure.Err.Error(), deliveryValue) {
		t.Fatalf("Error() leaks the secret: %q", result.Failure.Err)
	}
	var execErr *shell.ExecutionError
	if !errors.As(result.Failure.Err, &execErr) {
		t.Fatal("dependents lost the typed shell.ExecutionError")
	}
	if execErr.Kind != shell.FailureExit || execErr.Result.ExitCode != 3 || execErr.NodeID != "a" {
		t.Fatalf("typed evidence changed: %#v", execErr)
	}
	for name, text := range map[string]string{"stdout": execErr.Result.Stdout, "stderr": execErr.Result.Stderr, "error": execErr.Error()} {
		if strings.Contains(text, deliveryValue) {
			t.Errorf("errors.As target leaks the secret in %s: %q", name, text)
		}
	}
	for chained := errors.Unwrap(result.Failure.Err); chained != nil; chained = errors.Unwrap(chained) {
		if strings.Contains(chained.Error(), deliveryValue) {
			t.Fatalf("unwrap chain leaks the secret: %q", chained)
		}
	}
}
