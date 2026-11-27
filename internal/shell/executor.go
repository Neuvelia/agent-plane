// Package shell executes local processes for shell nodes. It does not invoke a
// command interpreter or provide process isolation.
package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"agentplane/internal/domain/graph"
	"agentplane/internal/fields"
	"agentplane/internal/proctree"
)

// Executor satisfies runtime.Executor without adding process details to the
// scheduler. It has no mutable state and is safe to use for concurrent nodes.
// The zero value is valid.
type Executor struct {
	// Policy tunes termination of a canceled node's process tree. The zero
	// value selects proctree's defaults.
	Policy proctree.Policy

	// CaptureLimit is the number of leading bytes of each stream a Result
	// retains (ADR-0039). Zero selects DefaultCaptureLimit. It does not limit
	// what the log observer receives.
	CaptureLimit int

	// terminate replaces tree termination in tests to make a leak deterministic.
	terminate func(*proctree.Tree, context.Context, proctree.Policy) (proctree.Outcome, error)
}

// Result is the process evidence retained by Execute and ExecutionError.
// ExitCode is -1 if no OS exit code is available. A canceled process may
// report an OS-specific termination code. Each stream keeps at most the
// capture limit of leading bytes; StdoutOmitted and StderrOmitted count the
// bytes dropped beyond it, and a truncated stream's text ends with an omission
// marker (ADR-0039).
type Result struct {
	Stdout        string
	Stderr        string
	StdoutOmitted int64
	StderrOmitted int64
	ExitCode      int
	StartedAt     time.Time
	EndedAt       time.Time
}

// FailureKind distinguishes configuration, start, exit, cancellation, and
// structured-output failures without changing the generic runtime result.
type FailureKind string

const (
	FailureConfig   FailureKind = "config"
	FailureStart    FailureKind = "start"
	FailureExit     FailureKind = "exit"
	FailureCanceled FailureKind = "canceled"
	FailureOutput   FailureKind = "output"
	// FailureShutdown means cancellation could not empty the process tree.
	FailureShutdown FailureKind = "shutdown"
)

// ExecutionError carries process evidence even when ExecuteNode cannot return
// successful graph outputs. Cause remains available through errors.Unwrap.
type ExecutionError struct {
	NodeID string
	Kind   FailureKind
	Result Result
	Cause  error
	// Termination records how the process tree was stopped, if it was.
	Termination *proctree.Outcome
}

// ProcessTermination returns the recorded tree termination, or nil.
func (e *ExecutionError) ProcessTermination() *proctree.Outcome { return e.Termination }

func (e *ExecutionError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("shell node %q: %s", e.NodeID, e.Kind)
	}
	return fmt.Sprintf("shell node %q: %s: %v", e.NodeID, e.Kind, e.Cause)
}

func (e *ExecutionError) Unwrap() error { return e.Cause }

type config struct {
	command      string
	args         []string
	cwd          string
	envAllowlist []string
	env          map[string]string
	inputEnv     map[string]string
	secretEnv    map[string]string
	stdoutJSON   bool
}

// Execute runs one local process and returns its raw result. It never treats
// argument text as shell syntax. Only inputs listed in input_env enter the
// child process environment.
func (e Executor) Execute(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (Result, error) {
	return e.execute(ctx, Request{NodeID: nodeID, Node: node, Inputs: inputs})
}

func (e Executor) execute(ctx context.Context, request Request) (Result, error) {
	nodeID, node := request.NodeID, request.Node
	result := Result{ExitCode: -1}
	if ctx == nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: errors.New("context is nil")}
	}
	cfg, err := parseConfig(node)
	if err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: err}
	}
	grantedEnvironment, err := secretEnvironment(cfg, request.Secrets)
	if err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureCanceled, Result: result, Cause: err}
	}
	inputEnvironment, err := buildInputEnvironment(cfg, node, request.Inputs)
	if err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: err}
	}

	cmd := newCommand(cfg, request.Attempt, inputEnvironment, grantedEnvironment)
	// A descendant may inherit stdout or stderr after the direct child exits.
	// Bound the pipe drain so Wait can finish even if a descendant escapes.
	cmd.WaitDelay = time.Second
	stdout, stderr := newBoundedCapture(e.captureLimit()), newBoundedCapture(e.captureLimit())
	cmd.Stdout = &logWriter{capture: stdout, stream: "stdout", observe: request.Observe}
	cmd.Stderr = &logWriter{capture: stderr, stream: "stderr", observe: request.Observe}
	tree, err := proctree.Start(cmd)
	if err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureStart, Result: result, Cause: err}
	}
	result.StartedAt = time.Now()

	exited := make(chan struct{})
	stopped := watchCancellation(ctx, tree, e, exited)
	err = tree.Wait()
	close(exited)
	cancellation := <-stopped
	result.EndedAt = time.Now()
	result.Stdout, result.StdoutOmitted = stdout.text("stdout")
	result.Stderr, result.StderrOmitted = stderr.text("stderr")
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	failure := classify(ctx, err, cancellation)
	if failure == nil {
		return result, nil
	}
	failure.NodeID, failure.Result, failure.Termination = nodeID, result, cancellation.record()
	return result, failure
}

func (e Executor) captureLimit() int {
	if e.CaptureLimit > 0 {
		return e.CaptureLimit
	}
	return DefaultCaptureLimit
}

// newCommand builds the child with an explicit environment: configured values,
// mapped inputs, the granted secrets, then the loop attempt number.
func newCommand(cfg config, attempt int, inputEnvironment, grantedEnvironment []string) *exec.Cmd {
	cmd := exec.Command(cfg.command, cfg.args...)
	cmd.Dir = cfg.cwd
	cmd.Env = append(buildEnvironment(cfg), inputEnvironment...)
	cmd.Env = append(cmd.Env, grantedEnvironment...)
	if attempt > 0 {
		cmd.Env = append(cmd.Env, AttemptEnv+"="+strconv.Itoa(attempt))
	}
	return cmd
}

type cancellation struct {
	requested bool
	outcome   proctree.Outcome
	err       error
}

func (c *cancellation) record() *proctree.Outcome {
	if !c.requested {
		return nil
	}
	return &c.outcome
}

// watchCancellation terminates the tree when ctx is done before exited closes.
// The result channel delivers once, so the caller reads it without a race.
func watchCancellation(ctx context.Context, tree *proctree.Tree, e Executor, exited <-chan struct{}) <-chan cancellation {
	result := make(chan cancellation, 1)
	go func() {
		var c cancellation
		select {
		case <-ctx.Done():
			c.requested = true
			terminate := e.terminate
			if terminate == nil {
				terminate = (*proctree.Tree).Terminate
			}
			c.outcome, c.err = terminate(tree, context.Background(), e.Policy)
		case <-exited:
		}
		result <- c
	}()
	return result
}

// classify decides the failure kind from the termination record, not the exit
// status: a child that traps the request may exit 0 or nonzero. A tree that
// ended on its own before the request is not a cancellation. It returns nil for
// success.
func classify(ctx context.Context, waitErr error, c cancellation) *ExecutionError {
	termErr, outcome := c.err, c.outcome
	var leak *proctree.LeakError
	switch {
	case errors.As(termErr, &leak):
		return &ExecutionError{Kind: FailureShutdown, Cause: termErr}
	case c.requested && termErr == nil && (waitErr != nil || outcome.Method != proctree.MethodAlreadyExited):
		return &ExecutionError{Kind: FailureCanceled, Cause: ctx.Err()}
	case errors.Is(waitErr, exec.ErrWaitDelay):
		return &ExecutionError{Kind: FailureOutput, Cause: waitErr}
	case waitErr != nil:
		return &ExecutionError{Kind: FailureExit, Cause: waitErr}
	}
	return nil
}

// ExecuteNode adapts process output to explicitly declared node output ports.
// A failed process returns no accepted outputs; its evidence is in the error.
func (e Executor) ExecuteNode(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
	return e.ExecuteNodeWithLogs(ctx, nodeID, node, inputs, nil)
}

// ExecuteNodeWithLogs streams every byte in bounded chunks while retaining
// stdout and stderr evidence up to the capture limit. The observer must
// tolerate concurrent calls from the two output-copy goroutines.
func (e Executor) ExecuteNodeWithLogs(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
	return e.ExecuteNodeAttempt(ctx, nodeID, node, inputs, observe, 0)
}

// AttemptEnv is the reserved child variable carrying the 1-based loop attempt
// number (ADR-0018). It is only set for loop producers and cannot be set by
// node configuration.
const AttemptEnv = "AGENTPLANE_ATTEMPT"

// ValidateNodeConfig checks static shell configuration without running it.
func (Executor) ValidateNodeConfig(id string, node graph.NodeDefinition) error {
	if _, err := parseConfig(node); err != nil {
		return fmt.Errorf("shell node %s: %w", keyText(id), err)
	}
	return nil
}

// ExecuteNodeAttempt is ExecuteNodeWithLogs for a loop producer attempt.
// attempt 0 means "not a loop attempt" and adds no variable. It grants no
// secrets: a node that configures secret_env must be run through Run.
func (e Executor) ExecuteNodeAttempt(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte), attempt int) (map[string]graph.Value, error) {
	return e.Run(ctx, Request{NodeID: nodeID, Node: node, Inputs: inputs, Observe: observe, Attempt: attempt})
}

// checkOutputs returns the declared output names, sorted, or the first one a
// Shell node cannot produce.
func checkOutputs(node graph.NodeDefinition, cfg config) ([]string, error) {
	names := make([]string, 0, len(node.Outputs))
	for name := range node.Outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		switch name {
		case "stdout", "stderr", "exit_code":
		case "json":
			if !cfg.stdoutJSON {
				return nil, errors.New("json output requires stdout_json: true")
			}
		default:
			return nil, fmt.Errorf("unsupported shell output %q", name)
		}
	}
	return names, nil
}

const maxLogChunk = 16 * 1024

type logWriter struct {
	capture *boundedCapture
	stream  string
	observe func(string, []byte)
}

func (w *logWriter) Write(data []byte) (int, error) {
	n, err := w.capture.Write(data)
	if err != nil {
		return n, err
	}
	if w.observe != nil {
		for start := 0; start < n; start += maxLogChunk {
			end := start + maxLogChunk
			if end > n {
				end = n
			}
			w.observe(w.stream, data[start:end])
		}
	}
	return n, nil
}

func parseConfig(node graph.NodeDefinition) (config, error) {
	if node.Type != "shell" {
		return config{}, fmt.Errorf("node type must be shell, got %s", keyText(node.Type))
	}
	keys := make([]string, 0, len(node.Config))
	for key := range node.Config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	allowed := fields.AllowedKeys(ConfigFields(), node.Config)
	for _, key := range keys {
		if !allowed[key] {
			return config{}, fmt.Errorf("unknown shell config field %s", keyText(key))
		}
	}
	var cfg config
	command, ok := node.Config["command"].(string)
	if !ok || command == "" {
		return cfg, errors.New("command must be a nonempty string")
	}
	cfg.command = command
	var err error
	if value, present := node.Config["args"]; present {
		cfg.args, err = stringList(value, "args")
		if err != nil {
			return cfg, err
		}
	}
	if value, present := node.Config["cwd"]; present {
		cfg.cwd, ok = value.(string)
		if !ok {
			return cfg, errors.New("cwd must be a string")
		}
	}
	if value, present := node.Config["env_allowlist"]; present {
		cfg.envAllowlist, err = stringList(value, "env_allowlist")
		if err != nil {
			return cfg, err
		}
		for index, key := range cfg.envAllowlist {
			if !validEnvName(key) {
				return cfg, fmt.Errorf("env_allowlist[%d] is not a valid environment name; use a non-empty name without '='", index)
			}
		}
	}
	if value, present := node.Config["env"]; present {
		cfg.env, err = stringMap(value, "env", entryText)
		if err != nil {
			return cfg, err
		}
	}
	if value, present := node.Config["input_env"]; present {
		cfg.inputEnv, err = stringMap(value, "input_env", inputEntryText)
		if err != nil {
			return cfg, err
		}
		used := make(map[string]string, len(cfg.inputEnv))
		for _, inputID := range sortedMapKeys(cfg.inputEnv) {
			variable := cfg.inputEnv[inputID]
			port, declared := node.Inputs[inputID]
			if !declared {
				return cfg, fmt.Errorf("input_env refers to undeclared input %s", keyText(inputID))
			}
			if port.Type != graph.ValueTypeString {
				return cfg, fmt.Errorf("input_env input %s must be a string input", keyText(inputID))
			}
			if !validEnvName(variable) {
				return cfg, fmt.Errorf("input_env input %s has an invalid environment name; use a non-empty name without '='", keyText(inputID))
			}
			for usedVariable, other := range used {
				if strings.EqualFold(usedVariable, variable) {
					return cfg, fmt.Errorf("input_env has duplicate environment name %s for inputs %s and %s; names are compared case-insensitively", envText(variable, "<not shown>"), keyText(other), keyText(inputID))
				}
			}
			for explicit := range cfg.env {
				if strings.EqualFold(explicit, variable) {
					return cfg, fmt.Errorf("input_env input %s maps to environment name %s, which conflicts with env", keyText(inputID), envText(variable, "<not shown>"))
				}
			}
			for _, allowed := range cfg.envAllowlist {
				if strings.EqualFold(allowed, variable) {
					return cfg, fmt.Errorf("input_env input %s maps to environment name %s, which conflicts with env_allowlist", keyText(inputID), envText(variable, "<not shown>"))
				}
			}
			used[variable] = inputID
		}
	}
	if value, present := node.Config["secret_env"]; present {
		if cfg.secretEnv, err = parseSecretEnv(value, cfg); err != nil {
			return cfg, err
		}
	}
	if value, present := node.Config["stdout_json"]; present {
		cfg.stdoutJSON, ok = value.(bool)
		if !ok {
			return cfg, errors.New("stdout_json must be a boolean")
		}
	}
	// AGENTPLANE_ATTEMPT belongs to the runtime for every shell node (ADR-0018):
	// it may not be set, inherited from the host or mapped from an input, in any
	// letter case, so a child can always trust the value it sees.
	reserved := func(name string) bool { return strings.EqualFold(name, AttemptEnv) }
	for key := range cfg.env {
		if reserved(key) {
			return cfg, fmt.Errorf("env %s is reserved for the runtime", AttemptEnv)
		}
	}
	for _, key := range cfg.envAllowlist {
		if reserved(key) {
			return cfg, fmt.Errorf("env_allowlist %s is reserved for the runtime", AttemptEnv)
		}
	}
	for _, variable := range cfg.inputEnv {
		if reserved(variable) {
			return cfg, fmt.Errorf("input_env cannot map to %s, which is reserved for the runtime", AttemptEnv)
		}
	}
	return cfg, nil
}

func stringList(value any, field string) ([]string, error) {
	switch list := value.(type) {
	case []string:
		return append([]string(nil), list...), nil
	case []any:
		result := make([]string, len(list))
		for i, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be a string", field, i)
			}
			result[i] = text
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s must be a list of strings", field)
	}
}

// stringMap reads a mapping of strings; label names an entry in errors and
// decides which keys may be quoted.
func stringMap(value any, field string, label func(string, int, string) string) (map[string]string, error) {
	raw, keys, err := sortedEntries(value, field)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for index, key := range keys {
		item, ok := raw[key].(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", label(field, index, key))
		}
		if !validEnvName(key) {
			return nil, fmt.Errorf("%s has an invalid name; use a non-empty name without '='", label(field, index, key))
		}
		if strings.ContainsRune(item, 0) {
			return nil, fmt.Errorf("%s contains a NUL byte", label(field, index, key))
		}
		result[key] = item
	}
	return result, nil
}

// sortedEntries normalizes the accepted mapping shapes and returns the keys in
// sorted order.
func sortedEntries(value any, field string) (map[string]any, []string, error) {
	raw := map[string]any{}
	switch entries := value.(type) {
	case map[string]string:
		for key, item := range entries {
			raw[key] = item
		}
	case graph.NodeConfig:
		for key, item := range entries {
			raw[key] = item
		}
	case map[string]any:
		for key, item := range entries {
			raw[key] = item
		}
	default:
		return nil, nil, fmt.Errorf("%s must be a mapping of string values", field)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return raw, keys, nil
}

func validEnvName(name string) bool { return name != "" && !strings.ContainsAny(name, "=\x00") }

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func buildInputEnvironment(cfg config, node graph.NodeDefinition, inputs map[string]graph.Value) ([]string, error) {
	environment := make([]string, 0, len(cfg.inputEnv))
	for _, inputID := range sortedMapKeys(cfg.inputEnv) {
		value, present := inputs[inputID]
		if !present {
			if node.Inputs[inputID].Required {
				return nil, fmt.Errorf("input_env required input %s is missing", keyText(inputID))
			}
			continue
		}
		text, ok := value.AsString()
		if !ok {
			return nil, fmt.Errorf("input_env input %s requires a string value", keyText(inputID))
		}
		if strings.ContainsRune(text, 0) {
			return nil, fmt.Errorf("input_env input %s contains a NUL byte", keyText(inputID))
		}
		environment = append(environment, cfg.inputEnv[inputID]+"="+text)
	}
	return environment, nil
}

func buildEnvironment(cfg config) []string {
	allowed := make(map[string]string, len(cfg.envAllowlist))
	for _, key := range cfg.envAllowlist {
		if value, ok := os.LookupEnv(key); ok {
			allowed[key] = value
		}
	}
	allowedKeys := make([]string, 0, len(allowed))
	for key := range allowed {
		allowedKeys = append(allowedKeys, key)
	}
	sort.Strings(allowedKeys)
	environment := make([]string, 0, len(allowed)+len(cfg.env))
	for _, key := range allowedKeys {
		if _, explicit := cfg.env[key]; !explicit {
			environment = append(environment, key+"="+allowed[key])
		}
	}
	explicitKeys := make([]string, 0, len(cfg.env))
	for key := range cfg.env {
		explicitKeys = append(explicitKeys, key)
	}
	sort.Strings(explicitKeys)
	for _, key := range explicitKeys {
		environment = append(environment, key+"="+cfg.env[key])
	}
	return environment
}
