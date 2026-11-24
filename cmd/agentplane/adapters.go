package main

import (
	"context"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/gate"
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
	values, err := (shell.Executor{}).Run(ctx, shell.Request{NodeID: id, Node: n, Inputs: in, Observe: o.Log, Attempt: o.AttemptNumber, Secrets: o.SecretEnv})
	return values, nil, err
}

// ReceivesSecrets declares that Shell nodes read ExecutionObservation.SecretEnv.
// Agent nodes do not: until an adapter delivers secrets, a graph that assigns
// one to an Agent node fails preflight (F3.8, ADR-0029).
func (nodeExecutor) ReceivesSecrets(nodeType string) bool { return nodeType == shellNodeType }

func (e nodeExecutor) ValidateNodeConfig(id string, n graph.NodeDefinition) error {
	if n.Type == "gate" {
		return (gate.Executor{}).ValidateNodeConfig(id, n)
	}
	if n.Type == shellNodeType {
		return (shell.Executor{}).ValidateNodeConfig(id, n)
	}
	return e.agents.ValidateNodeConfig(id, n)
}
func (e nodeExecutor) PreflightNode(ctx context.Context, id string, n graph.NodeDefinition) error {
	if n.Type == shellNodeType {
		return nil // no provider capability to detect for a local process
	}
	return e.agents.PreflightNode(ctx, id, n)
}
