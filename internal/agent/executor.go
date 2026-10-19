package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"agentplane/internal/graph"
	"agentplane/internal/runtime"
)

// Executor adapts one Agent graph node to the provider-neutral contract.
// Adapters is an explicit per-caller lookup, not a global or dynamic registry.
type Executor struct{ Adapters map[AdapterID]AgentAdapter }

var _ runtime.Executor = Executor{}
var _ runtime.CorrelatedExecutor = Executor{}

func (e Executor) ExecuteNode(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
	return e.ExecuteNodeWithCorrelation(ctx, nodeID, node, inputs, runtime.ExecutionCorrelation{})
}

func (e Executor) ExecuteNodeWithCorrelation(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, ids runtime.ExecutionCorrelation) (map[string]graph.Value, error) {
	if ctx == nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: ErrorInvalidInput, Cause: errors.New("execution context is nil")}
	}
	cfg, err := parseNode(node, inputs)
	if err != nil {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorInvalidInput, Cause: err}
	}
	adapter, ok := e.Adapters[cfg.adapter]
	if !ok || adapter == nil || adapter.ID() != cfg.adapter {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorUnavailable, Cause: errors.New("adapter is not registered under its logical ID")}
	}
	if err := ctx.Err(); err != nil {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorExecution, Cause: err}
	}
	report, err := adapter.Detect(ctx)
	if err != nil {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorDetection, Cause: err}
	}
	if !report.Available {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorUnavailable, Cause: errors.New("adapter reported unavailable")}
	}
	if report.Workspace != nil && !*report.Workspace {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorUnsupported, Cause: errors.New("adapter does not support workspace execution")}
	}
	request := AgentExecutionInput{Task: cfg.task, Workspace: cfg.workspace, AllowedPaths: cfg.allowedPaths, ContextRefs: cfg.contextRefs, EnvironmentRefs: cfg.environmentRefs, RunID: ids.RunID, NodeRunID: ids.NodeRunID, AttemptID: ids.AttemptID}
	output, err := adapter.Execute(ctx, request)
	if err != nil {
		var executionErr *ExecutionError
		if errors.As(err, &executionErr) {
			return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: executionErr.Kind, Cause: err}
		}
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorExecution, Cause: err}
	}
	values, err := outputValues(node, output)
	if err != nil {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorExecution, Cause: err}
	}
	return values, nil
}

func outputValues(node graph.NodeDefinition, output AgentExecutionOutput) (map[string]graph.Value, error) {
	if len(output.RawMetadata) > 0 && !json.Valid(output.RawMetadata) {
		return nil, errors.New("agent raw metadata is not valid JSON")
	}
	for _, file := range output.ChangedFiles {
		if file.Path == "" || (file.Provenance != ProvenanceReported && file.Provenance != ProvenanceObserved) {
			return nil, errors.New("agent changed file requires a path and reported/observed provenance")
		}
	}
	for _, artifact := range output.Artifacts {
		if artifact.Ref == "" {
			return nil, errors.New("agent artifact reference is empty")
		}
	}
	values := make(map[string]graph.Value, len(node.Outputs))
	for name := range node.Outputs {
		switch name {
		case "summary":
			if output.Summary != "" {
				values[name] = graph.NewStringValue(output.Summary)
			}
		case "changed_files":
			if output.ChangedFiles != nil {
				value, err := jsonGraphValue(output.ChangedFiles)
				if err != nil {
					return nil, err
				}
				values[name] = value
			}
		case "artifacts":
			if output.Artifacts != nil {
				value, err := jsonGraphValue(output.Artifacts)
				if err != nil {
					return nil, err
				}
				values[name] = value
			}
		case "raw_metadata":
			if len(output.RawMetadata) > 0 {
				value, err := graph.NewJSONValue(output.RawMetadata)
				if err != nil {
					return nil, err
				}
				values[name] = value
			}
		case "usage":
			if output.Usage != nil {
				value, err := jsonGraphValue(output.Usage)
				if err != nil {
					return nil, err
				}
				values[name] = value
			}
		default:
			return nil, fmt.Errorf("unsupported agent output %q", name)
		}
	}
	return values, nil
}

func jsonGraphValue(value any) (graph.Value, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return graph.Value{}, err
	}
	return graph.NewJSONValue(data)
}
