package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/secrets"
)

// errSecretNotGranted is returned when a node configures secret_env but the run
// granted no value for at least one of its variables (F3.8, ADR-0029). The
// process is never started. The text names neither variable nor secret.
var errSecretNotGranted = errors.New("secret_env is configured but no secret value was granted for it; the command was not started")

// Request is one Shell node run. Secrets holds only the values the run granted
// to this node; it prints as a count and cannot be serialized.
type Request struct {
	NodeID  string
	Node    graph.NodeDefinition
	Inputs  map[string]graph.Value
	Observe func(string, []byte)
	// Attempt is the 1-based loop attempt, or 0 outside a loop (ADR-0018).
	Attempt int
	Secrets secrets.Env
}

// Run is ExecuteNodeAttempt with an explicit request: it adapts process output
// to the declared output ports and hands the granted secrets to the child.
func (e Executor) Run(ctx context.Context, request Request) (map[string]graph.Value, error) {
	node, nodeID := request.Node, request.NodeID
	cfg, configErr := parseConfig(node)
	if configErr != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: Result{ExitCode: -1}, Cause: configErr}
	}
	names, outputErr := checkOutputs(node, cfg)
	if outputErr != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: Result{ExitCode: -1}, Cause: outputErr}
	}
	result, err := e.execute(ctx, request)
	if err != nil {
		return nil, err
	}
	if truncated := checkComplete(names, result, e.captureLimit()); truncated != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureOutput, Result: result, Cause: truncated}
	}
	return collectOutputs(nodeID, node, names, result)
}

// checkComplete returns a CaptureLimitError for the first declared output, in
// sorted order, whose source stream was truncated. exit_code reads no stream.
func checkComplete(names []string, result Result, limit int) error {
	for _, name := range names {
		stream, omitted := "stdout", result.StdoutOmitted
		switch name {
		case "stderr":
			stream, omitted = "stderr", result.StderrOmitted
		case "exit_code":
			continue
		}
		if omitted > 0 {
			return &CaptureLimitError{Output: name, Stream: stream, Limit: limit, Omitted: omitted}
		}
	}
	return nil
}

// collectOutputs maps a finished process to the declared output ports.
func collectOutputs(nodeID string, node graph.NodeDefinition, names []string, result Result) (map[string]graph.Value, error) {
	outputs := make(map[string]graph.Value, len(node.Outputs))
	for _, name := range names {
		switch name {
		case "stdout":
			outputs[name] = graph.NewStringValue(result.Stdout)
		case "stderr":
			outputs[name] = graph.NewStringValue(result.Stderr)
		case "exit_code":
			value, _ := graph.NewNumberValue(json.Number(strconv.Itoa(result.ExitCode)))
			outputs[name] = value
		case "json":
			value, parseErr := graph.NewJSONValue(json.RawMessage(result.Stdout))
			if parseErr != nil {
				return nil, &ExecutionError{NodeID: nodeID, Kind: FailureOutput, Result: result, Cause: fmt.Errorf("stdout is not one complete JSON value: %w", parseErr)}
			}
			outputs[name] = value
		}
	}
	return outputs, nil
}

// secretEnvironment returns the NAME=value entries for the secret_env variables
// the node configures. Every configured variable needs a grant; a grant for a
// variable the node does not configure is ignored, never forwarded. It reports
// facts only and never quotes a variable name.
func secretEnvironment(cfg config, granted secrets.Env) ([]string, error) {
	if len(cfg.secretEnv) == 0 {
		return nil, nil
	}
	selected := granted.Pick(cfg.secretEnv)
	if len(selected) != len(cfg.secretEnv) {
		return nil, errSecretNotGranted
	}
	return selected, nil
}
