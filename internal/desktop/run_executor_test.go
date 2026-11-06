package desktop

import (
	"testing"

	"agentplane/internal/agent/codex"
)

func TestDesktopExecutionAndCapabilityUseSameSupervisor(t *testing.T) {
	const supervisor = "/trusted/agentplane-desktop"
	const binary = "/trusted/codex"
	environment := LaunchEnvironment{shell: map[string]string{"SHELL_KEY": "one"}, codex: map[string]string{"CODEX_KEY": "two"}}
	service := NewPreflightServiceWithSupervisor(nil, nil, nil, supervisor)
	installed, ok := service.detector.(installedCapabilities)
	if !ok || installed.supervisorBinary != supervisor || service.supervisorBinary != supervisor {
		t.Fatalf("capability and service supervisor differ: %#v", service)
	}
	shell := shellExecutorFor(environment, service.supervisorBinary)
	if shell.SupervisorBinary != supervisor || shell.FrozenEnvironment["SHELL_KEY"] != "one" {
		t.Fatalf("shell launch lost supervisor or frozen environment: %#v", shell)
	}
	chosen := binary
	settings := LocalSettings{CodexBinaryPath: &chosen}
	agent := agentExecutorFor(settings, environment, service.supervisorBinary).Adapters["codex"]
	adapter, ok := agent.(codex.Adapter)
	if !ok || adapter.Binary != binary || adapter.SupervisorBinary != supervisor || adapter.FrozenEnvironment["CODEX_KEY"] != "two" {
		t.Fatalf("Codex launch lost supervisor or frozen environment: %#v", agent)
	}
}
