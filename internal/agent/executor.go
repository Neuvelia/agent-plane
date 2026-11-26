package agent

import (
	"agentplane/internal/domain/run"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/runtime"
)

// Executor adapts one Agent graph node to the provider-neutral contract.
// Adapters is an explicit per-caller lookup, not a global or dynamic registry.
type Executor struct{ Adapters map[AdapterID]AgentAdapter }

var _ runtime.Executor = Executor{}
var _ runtime.CorrelatedExecutor = Executor{}

func (e Executor) ExecuteNode(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
	return e.ExecuteNodeWithCorrelation(ctx, nodeID, node, inputs, run.ExecutionCorrelation{})
}

// ExecuteNodeWithCorrelation runs the node with the scheduler-provided ids.
func (e Executor) ExecuteNodeWithCorrelation(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, ids run.ExecutionCorrelation) (map[string]graph.Value, error) {
	return e.execute(ctx, nodeID, node, inputs, ids, nil, nil, nil, nil)
}

func (e Executor) ExecuteNodeObserved(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, obs runtime.ExecutionObservation) (map[string]graph.Value, *events.NodeEvidencePayload, error) {
	var evidence *events.NodeEvidencePayload
	values, err := e.execute(ctx, nodeID, node, inputs, obs.Correlation, obs.Log, obs.Progress, &evidence, obs.Feedback)
	return values, evidence, err
}

//nolint:gocognit,gocyclo,funlen // legacy size, split in stage 3c of docs/audits/2026-11-16-clean-architecture-refactor-plan.md
func (e Executor) execute(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, ids run.ExecutionCorrelation, log func(string, []byte), normalized func(events.Type, events.Payload), evidence **events.NodeEvidencePayload, feedback *run.CorrectionFeedback) (map[string]graph.Value, error) {
	if ctx == nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: ErrorInvalidInput, Cause: errors.New("execution context is nil")}
	}
	cfg, err := parseNode(node, inputs)
	if err != nil {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorInvalidInput, Cause: err}
	}
	adapter, ok := e.Adapters[cfg.adapter]
	if !ok || adapter == nil || adapter.ID() != cfg.adapter {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorUnavailable, Cause: fmt.Errorf("adapter %q is not registered, so its CLI cannot be launched; run with --adapter codex --codex-bin ABSOLUTE_PATH to register the Codex adapter", cfg.adapter)}
	}
	if err := ctx.Err(); err != nil {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorExecution, Cause: err}
	}
	report, err := adapter.Detect(ctx)
	if err != nil {
		kind := ErrorDetection
		var typed *ExecutionError
		if errors.As(err, &typed) {
			kind = typed.Kind
		}
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: kind, Cause: err}
	}
	if !report.Available {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorUnavailable, Cause: unavailableCause(cfg.adapter, report)}
	}
	if report.Workspace != nil && !*report.Workspace {
		return nil, &ExecutionError{NodeID: nodeID, AdapterID: cfg.adapter, Kind: ErrorUnsupported, Cause: errors.New("adapter does not support workspace execution")}
	}
	if node.Loop != nil && (report.CorrectionFeedback == nil || !*report.CorrectionFeedback) {
		return nil, &ExecutionError{NodeID: nodeID, Kind: ErrorUnsupported, Cause: errors.New("adapter does not affirm correction feedback transport")}
	}
	request := AgentExecutionInput{Feedback: run.CloneFeedback(feedback), Task: cfg.task, Workspace: cfg.workspace, AllowedPaths: cfg.allowedPaths, ContextRefs: cfg.contextRefs, EnvironmentRefs: cfg.environmentRefs, RunID: ids.RunID, NodeRunID: ids.NodeRunID, AttemptID: ids.AttemptID}
	var output AgentExecutionOutput
	if progress, ok := adapter.(NormalizedProgressAdapter); ok && normalized != nil {
		output, err = progress.ExecuteWithNormalizedProgress(ctx, request, normalized)
	} else if progress, ok := adapter.(ProgressAdapter); ok {
		output, err = progress.ExecuteWithProgress(ctx, request, log)
	} else {
		output, err = adapter.Execute(ctx, request)
	}
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
	if evidence != nil && output.ChangedFiles != nil {
		p := events.NodeEvidencePayload{AdapterID: string(cfg.adapter), ChangedFiles: []events.ChangedFile{}}
		if report.Version != nil {
			p.AdapterVersion = *report.Version
		}
		for _, f := range output.ChangedFiles {
			p.ChangedFiles = append(p.ChangedFiles, events.ChangedFile{Path: f.Path, Provenance: string(f.Provenance)})
		}
		*evidence = &p
	}
	return values, nil
}

// unavailableCause names the missing CLI and tells the user how to fix it.
func unavailableCause(id AdapterID, report CapabilityReport) error {
	name := report.DisplayName
	if name == "" {
		name = string(id)
	}
	return fmt.Errorf("%s was not found at the configured binary path; install it and run with --adapter %s --codex-bin ABSOLUTE_PATH pointing at the installed executable", name, id)
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

// ValidateNodeConfig is side-effect free, including for a task bound at runtime.
func (e Executor) ValidateNodeConfig(_ string, n graph.NodeDefinition) error {
	in := map[string]graph.Value{}
	if _, ok := n.Config["task"]; !ok {
		in["task"] = graph.NewStringValue("preflight unresolved task")
	}
	_, err := parseNode(n, in)
	return err
}
func (e Executor) PreflightNode(ctx context.Context, _ string, n graph.NodeDefinition) error {
	id, _ := n.Config["adapter"].(string)
	a := e.Adapters[AdapterID(id)]
	if a == nil || a.ID() != AdapterID(id) {
		return errors.New("loop adapter is unavailable")
	}
	r, err := a.Detect(ctx)
	if err != nil {
		return err
	}
	if !r.Available || (r.Workspace != nil && !*r.Workspace) || r.CorrectionFeedback == nil || !*r.CorrectionFeedback {
		return errors.New("loop requires available workspace adapter with affirmative correction-feedback transport capability")
	}
	return nil
}
