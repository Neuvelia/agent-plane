package main

import (
	"context"

	"agentplane/internal/agent"
	"agentplane/internal/events"
	"agentplane/internal/gate"
	"agentplane/internal/graph"
	"agentplane/internal/runtime"
	"agentplane/internal/shell"
)

// Composition belongs at the caller boundary, never inside the scheduler.
type nodeExecutor struct{ agents agent.Executor }

func (e nodeExecutor) ExecuteNode(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value) (map[string]graph.Value, error) {
	values, _, err := e.ExecuteNodeObserved(ctx, id, n, in, runtime.ExecutionObservation{})
	return values, err
}
func (e nodeExecutor) ExecuteNodeObserved(ctx context.Context, id string, n graph.NodeDefinition, in map[string]graph.Value, o runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	if n.Type == "gate" {
		return (gate.Executor{}).ExecuteNodeObserved(ctx, id, n, in, o)
	}
	if n.Type == "agent" {
		return e.agents.ExecuteNodeObserved(ctx, id, n, in, o)
	}
	values, err := (shell.Executor{}).ExecuteNodeWithLogs(ctx, id, n, in, o.Log)
	return values, nil, err
}

func (e nodeExecutor) ValidateNodeConfig(id string, n graph.NodeDefinition) error {
	if n.Type == "gate" {
		return (gate.Executor{}).ValidateNodeConfig(id, n)
	}
	return e.agents.ValidateNodeConfig(id, n)
}
func (e nodeExecutor) PreflightNode(ctx context.Context, id string, n graph.NodeDefinition) error {
	return e.agents.PreflightNode(ctx, id, n)
}
