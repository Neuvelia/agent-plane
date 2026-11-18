// Package shell executes local processes for shell nodes. It does not invoke a
// command interpreter or provide process isolation.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
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

	// terminate replaces tree termination in tests to make a leak deterministic.
	terminate func(*proctree.Tree, context.Context, proctree.Policy) (proctree.Outcome, error)
}

// Result is the process evidence retained by Execute and ExecutionError.
// ExitCode is -1 if no OS exit code is available. A canceled process may
// report an OS-specific termination code.
type Result struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	StartedAt time.Time
	EndedAt   time.Time
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
	stdoutJSON   bool
}

// Execute runs one local process and returns its raw result. It never treats
// argument text as shell syntax. Only inputs listed in input_env enter the
// child process environment.
func (e Executor) Execute(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (Result, error) {
	return execute(ctx, nodeID, node, inputs, nil, 0, e)
}

func execute(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte), attempt int, e Executor) (Result, error) {
	result := Result{ExitCode: -1}
	if ctx == nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: errors.New("context is nil")}
	}
	cfg, err := parseConfig(node)
	if err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: err}
	}
	if err := ctx.Err(); err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureCanceled, Result: result, Cause: err}
	}
	inputEnvironment, err := buildInputEnvironment(cfg, node, inputs)
	if err != nil {
		return result, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: result, Cause: err}
	}

	cmd := exec.Command(cfg.command, cfg.args...)
	cmd.Dir = cfg.cwd
	cmd.Env = append(buildEnvironment(cfg), inputEnvironment...)
	if attempt > 0 {
		cmd.Env = append(cmd.Env, AttemptEnv+"="+strconv.Itoa(attempt))
	}
	// A descendant may inherit stdout or stderr after the direct child exits.
	// Bound the pipe drain so Wait can finish even if a descendant escapes.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &logWriter{capture: &stdout, stream: "stdout", observe: observe}
	cmd.Stderr = &logWriter{capture: &stderr, stream: "stderr", observe: observe}
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
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
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

// ExecuteNodeWithLogs streams bounded chunks while retaining F0.5's complete
// stdout and stderr result evidence. The observer must tolerate concurrent
// calls from the two output-copy goroutines.
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
		return fmt.Errorf("shell node %q: %w", id, err)
	}
	return nil
}

// ExecuteNodeAttempt is ExecuteNodeWithLogs for a loop producer attempt.
// attempt 0 means "not a loop attempt" and adds no variable.
func (e Executor) ExecuteNodeAttempt(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte), attempt int) (map[string]graph.Value, error) {
	cfg, configErr := parseConfig(node)
	if configErr != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: Result{ExitCode: -1}, Cause: configErr}
	}
	names, outputErr := checkOutputs(node, cfg)
	if outputErr != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: Result{ExitCode: -1}, Cause: outputErr}
	}
	result, err := execute(ctx, nodeID, node, inputs, observe, attempt, e)
	if err != nil {
		return nil, err
	}
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
	capture *bytes.Buffer
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
		return config{}, fmt.Errorf("node type must be shell, got %q", node.Type)
	}
	keys := make([]string, 0, len(node.Config))
	for key := range node.Config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	allowed := fields.AllowedKeys(ConfigFields(), node.Config)
	for _, key := range keys {
		if !allowed[key] {
			return config{}, fmt.Errorf("unknown shell config field %q", key)
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
		for _, key := range cfg.envAllowlist {
			if !validEnvName(key) {
				return cfg, fmt.Errorf("invalid env_allowlist name %q", key)
			}
		}
	}
	if value, present := node.Config["env"]; present {
		cfg.env, err = stringMap(value, "env")
		if err != nil {
			return cfg, err
		}
	}
	if value, present := node.Config["input_env"]; present {
		cfg.inputEnv, err = stringMap(value, "input_env")
		if err != nil {
			return cfg, err
		}
		used := make(map[string]string, len(cfg.inputEnv))
		for _, inputID := range sortedMapKeys(cfg.inputEnv) {
			variable := cfg.inputEnv[inputID]
			port, declared := node.Inputs[inputID]
			if !declared {
				return cfg, fmt.Errorf("input_env refers to undeclared input %q", inputID)
			}
			if port.Type != graph.ValueTypeString {
				return cfg, fmt.Errorf("input_env input %q must be a string input", inputID)
			}
			if !validEnvName(variable) {
				return cfg, fmt.Errorf("input_env input %q has invalid environment name %q", inputID, variable)
			}
			for usedVariable, other := range used {
				if strings.EqualFold(usedVariable, variable) {
					return cfg, fmt.Errorf("input_env has duplicate environment name %q for inputs %q and %q", variable, other, inputID)
				}
			}
			for explicit := range cfg.env {
				if strings.EqualFold(explicit, variable) {
					return cfg, fmt.Errorf("input_env environment name %q conflicts with env", variable)
				}
			}
			for _, allowed := range cfg.envAllowlist {
				if strings.EqualFold(allowed, variable) {
					return cfg, fmt.Errorf("input_env environment name %q conflicts with env_allowlist", variable)
				}
			}
			used[variable] = inputID
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

func stringMap(value any, field string) (map[string]string, error) {
	result := map[string]string{}
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
		return nil, fmt.Errorf("%s must be a mapping of string values", field)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item, ok := raw[key].(string)
		if !ok {
			return nil, fmt.Errorf("%s[%q] must be a string", field, key)
		}
		if !validEnvName(key) {
			return nil, fmt.Errorf("invalid %s name %q", field, key)
		}
		if strings.ContainsRune(item, 0) {
			return nil, fmt.Errorf("%s[%q] contains a NUL byte", field, key)
		}
		result[key] = item
	}
	return result, nil
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
				return nil, fmt.Errorf("input_env required input %q is missing", inputID)
			}
			continue
		}
		text, ok := value.AsString()
		if !ok {
			return nil, fmt.Errorf("input_env input %q requires a string value", inputID)
		}
		if strings.ContainsRune(text, 0) {
			return nil, fmt.Errorf("input_env input %q contains a NUL byte", inputID)
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
