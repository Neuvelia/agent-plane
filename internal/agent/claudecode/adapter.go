// Package claudecode implements the explicitly selected, installed Claude Code
// CLI protocol in non-interactive print mode (`claude -p`).
//
// Every protocol detail that comes from the F1.2b plan rather than from a run
// against the real CLI is marked ASSUMED(spike) so the pending spike (plan
// step 0) can find and confirm it. Nothing marked so has been verified.
package claudecode

import (
	"context"
	"errors"
	"os"
	"strings"

	"agentplane/internal/agent"
	"agentplane/internal/domain/events"
	"agentplane/internal/proctree"
)

// maxRecord bounds one stream-json record, one capability probe response and
// the final structured result.
const maxRecord = 1 << 20

// resultSchema is the JSON Schema handed to --json-schema.
// ASSUMED(spike): the CLI accepts this schema inline and reports a conforming
// value in the `structured_output` field of the final `result` record.
const resultSchema = `{"type":"object","additionalProperties":false,"required":["summary","artifacts"],"properties":{"summary":{"type":"string"},"artifacts":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["ref","name"],"properties":{"ref":{"type":"string"},"name":{"type":"string"}}}}}}`

// Adapter never forwards the ambient environment. Binary must be absolute.
type Adapter struct {
	Binary string
	// Workspaces supplies the Git workspace evidence; the composition root
	// provides it so this package imports no other adapter. Execute fails with
	// claude_workspace when it is nil.
	Workspaces Workspaces
	// Termination tunes how a cancelled process tree is stopped. The zero value
	// selects proctree's defaults.
	Termination proctree.Policy
}

// ID returns the logical adapter identifier used by graphs and the CLI.
func (Adapter) ID() agent.AdapterID { return "claude" }

// Error retains a cause for programmatic inspection but prints only a safe message.
type Error struct {
	Code, Message string
	cause         error
}

func (e *Error) Error() string { return "claude " + e.Code + ": " + e.Message }

// Unwrap exposes the retained cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.cause }

// DiagnosticCode maps the error to the finite claude_* vocabulary.
func (e *Error) DiagnosticCode() string {
	switch e.Code {
	case "detection", "unsupported_platform", "unsupported_capability", "environment", "workspace", "git", "start", "shutdown", "cancelled", "process_exit", "result", "progress":
		return "claude_" + e.Code
	default:
		return "claude_process_exit"
	}
}

func fail(code, message string, cause error) error {
	return &agent.ExecutionError{AdapterID: "claude", Kind: agent.ErrorKind("claude_" + code), Cause: &Error{Code: code, Message: message, cause: cause}}
}

// Execute runs the task without progress observation.
func (a Adapter) Execute(ctx context.Context, in agent.AgentExecutionInput) (agent.AgentExecutionOutput, error) {
	return a.execute(ctx, in, nil)
}

// ExecuteWithNormalizedProgress runs the task and reports normalized public
// progress. Provider records, reasoning, tool contents and stderr stay private.
func (a Adapter) ExecuteWithNormalizedProgress(ctx context.Context, in agent.AgentExecutionInput, normalized func(events.Type, events.Payload)) (agent.AgentExecutionOutput, error) {
	return a.execute(ctx, in, normalized)
}

// invocation is everything prepared before the child starts.
type invocation struct {
	workspace string
	prompt    string
	env       []string
	private   string
	observed  Observation
}

func (a Adapter) execute(ctx context.Context, in agent.AgentExecutionInput, normalized func(events.Type, events.Payload)) (agent.AgentExecutionOutput, error) {
	var out agent.AgentExecutionOutput
	inv, err := a.prepare(ctx, in)
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(inv.private)
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := newProgress(a.Workspaces, inv.workspace, normalized, cancel)
	err = runProcess(pctx, a.Binary, arguments(), inv.env, inv.workspace, strings.NewReader(inv.prompt), stream, a.Termination)
	if err = processFailure(ctx, stream, err); err != nil {
		return out, err
	}
	if err = stream.finish(); err != nil {
		return out, err
	}
	files, err := changed(ctx, inv.observed)
	if err != nil {
		return out, err
	}
	result := *stream.result
	result.ChangedFiles = files
	return result, nil
}

// prepare validates every input and creates the private directory last, so a
// failure before that point leaves nothing to clean up.
func (a Adapter) prepare(ctx context.Context, in agent.AgentExecutionInput) (invocation, error) {
	var inv invocation
	prompt, err := buildPrompt(in)
	if err != nil {
		return inv, err
	}
	env, err := environment(in.EnvironmentRefs)
	if err != nil {
		return inv, err
	}
	workspace, err := a.canonicalWorkspace(ctx, in.Workspace)
	if err != nil {
		return inv, err
	}
	observed, err := a.begin(ctx, workspace)
	if err != nil {
		return inv, err
	}
	home, err := newPrivateHome(workspace)
	if err != nil {
		return inv, err
	}
	return invocation{workspace: workspace, prompt: prompt, env: append(env, adapterEnvironment(home)...), private: home, observed: observed}, nil
}

// processFailure maps a stream rejection or a process failure to a safe error.
// A stream rejection wins: it cancelled the process itself.
func processFailure(ctx context.Context, stream *progress, err error) error {
	if stream.err != nil {
		return stream.err
	}
	if err == nil {
		return nil
	}
	var typed *agent.ExecutionError
	if errors.As(err, &typed) {
		return err
	}
	if ctx.Err() != nil {
		return fail("cancelled", "execution cancelled", err)
	}
	return fail("process_exit", "Claude Code execution failed; raw provider output withheld", err)
}

var _ agent.AgentAdapter = Adapter{}
var _ agent.NormalizedProgressAdapter = Adapter{}
