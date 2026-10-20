package desktop

import (
	"context"

	"agentplane/internal/agent"
	"agentplane/internal/agent/codex"
	"agentplane/internal/events"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
)

// desktopNodeExecutor composes existing execution types at the sidecar caller
// boundary. The runtime scheduler remains independent of provider choices.
type desktopNodeExecutor struct {
	shell  shell.Executor
	agents agent.Executor
}

func shellExecutorFor(environment LaunchEnvironment) shell.Executor {
	return shell.Executor{FrozenEnvironment: environment.ShellValues()}
}

func agentExecutorFor(settings LocalSettings, environment LaunchEnvironment) agent.Executor {
	executor := agent.Executor{Adapters: make(map[agent.AdapterID]agent.AgentAdapter)}
	if settings.CodexBinaryPath != nil {
		executor.Adapters["codex"] = codex.Adapter{Binary: *settings.CodexBinaryPath,
			FrozenEnvironment: environment.CodexValues()}
	}
	return executor
}

func (e desktopNodeExecutor) ExecuteNode(ctx context.Context, id string, node graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
	values, _, err := e.ExecuteNodeObserved(ctx, id, node, inputs, runtime.ExecutionObservation{})
	return values, err
}

func (e desktopNodeExecutor) ExecuteNodeObserved(ctx context.Context, id string, node graph.NodeDefinition, inputs map[string]graph.Value, observation runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	switch node.Type {
	case "gate":
		return (gate.Executor{}).ExecuteNodeObserved(ctx, id, node, inputs, observation)
	case "agent":
		return e.agents.ExecuteNodeObserved(ctx, id, node, inputs, observation)
	default:
		values, err := e.shell.ExecuteNodeWithLogs(ctx, id, node, inputs, observation.Log)
		return values, nil, err
	}
}

func (e desktopNodeExecutor) ValidateNodeConfig(id string, node graph.NodeDefinition) error {
	if node.Type == "gate" {
		return (gate.Executor{}).ValidateNodeConfig(id, node)
	}
	return e.agents.ValidateNodeConfig(id, node)
}

func (e desktopNodeExecutor) PreflightNode(ctx context.Context, id string, node graph.NodeDefinition) error {
	return e.agents.PreflightNode(ctx, id, node)
}
