package main

import (
	"context"
	"fmt"
	"io"

	"agentplane/internal/adapter/system"
	"agentplane/internal/adapter/yamlgraph"
	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	"agentplane/internal/domain/run"
	"agentplane/internal/runtime"
)

// selfCheckGraph is the runner self-check (F2.11a): one Shell node whose
// bounded loop is rejected by an embedded gate on attempt 1 and accepted on
// attempt 2. It proves, with the same runtime `run` uses, that the shell
// starts, its output is parsed, the gate decides and exactly one retry happens.
// The command only prints text; nothing is read, written, persisted or sent.
const selfCheckGraph = `schema_version: 0
id: self-check
name: Runner self-check
inputs: {}
nodes:
  check:
    type: shell
    config:
      command: /bin/sh
      args:
        - -c
        - 'if [ "$AGENTPLANE_ATTEMPT" = 1 ]; then printf "{\"ready\":false}"; else printf "{\"ready\":true}"; fi'
      stdout_json: true
    outputs:
      json: {type: json, required: true}
    loop:
      max_attempts: 3
      gate:
        inputs:
          candidate: {type: json, required: true}
        config:
          check_id: check-ready
          kind: json_predicate
          input: candidate
          field: [ready]
          operator: eq
          expected: true
      bindings:
        - {from: candidate.outputs.json, to: gate.inputs.candidate}
edges: []
outputs:
  result: {from: check.outputs.json}
`

// selfCheckCommand runs the embedded self-check graph and reports pass or fail.
// It takes no arguments and persists nothing.
func selfCheckCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		return usageError("self-check", fmt.Errorf("self-check takes no arguments"), false, stdout, stderr)
	}
	result, err := runSelfCheck(ctx)
	if err != nil {
		return commandError("self-check", "error", "internal", err, nil, false, stdout, stderr, exitInternal)
	}
	if result.State != run.RunSucceeded {
		if _, werr := fmt.Fprintf(stderr, "self-check failed: the runner finished in state %q\n", result.State); werr != nil {
			return presentationError(werr, stderr)
		}
		return exitRun
	}
	if err := verifyOneRetry(result.Events); err != nil {
		if _, werr := fmt.Fprintf(stderr, "self-check failed: %v\n", err); werr != nil {
			return presentationError(werr, stderr)
		}
		return exitRun
	}
	if _, err := fmt.Fprintln(stdout, "self-check passed: shell node, bounded loop, gate and one retry ran"); err != nil {
		return presentationError(err, stderr)
	}
	return exitOK
}

// verifyOneRetry requires the attempt outcomes to be exactly rejected then
// accepted: a succeeded run alone would also pass if the retry never happened.
func verifyOneRetry(log []events.Event) error {
	var outcomes []string
	for _, event := range log {
		if payload, ok := event.Payload.(events.AttemptFinishedPayload); ok {
			outcomes = append(outcomes, payload.Outcome)
		}
	}
	if len(outcomes) != 2 || outcomes[0] != "rejected" || outcomes[1] != "accepted" {
		return fmt.Errorf("expected one rejected then one accepted attempt, observed %v", outcomes)
	}
	return nil
}

func runSelfCheck(ctx context.Context) (run.Result, error) {
	def, err := yamlgraph.Parse([]byte(selfCheckGraph))
	if err != nil {
		return run.Result{}, fmt.Errorf("parse the embedded self-check graph: %w", err)
	}
	if validation := graph.Validate(def); validation.HasErrors() {
		return run.Result{}, fmt.Errorf("the embedded self-check graph is invalid")
	}
	opts := runtime.Options{GraphVersion: yamlgraph.Marshal, Clock: system.Clock{}, IDs: system.IDs{}, MaxConcurrency: 1}
	return runtime.Run(ctx, def, nil, nodeExecutor{}, opts)
}
