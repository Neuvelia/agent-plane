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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"agentplane/internal/graph"
)

// Executor satisfies runtime.Executor without adding process details to the
// scheduler. It has no mutable state and is safe to use for concurrent nodes.
type Executor struct{}

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
)

// ExecutionError carries process evidence even when ExecuteNode cannot return
// successful graph outputs. Cause remains available through errors.Unwrap.
type ExecutionError struct {
	NodeID string
	Kind   FailureKind
	Result Result
	Cause  error
}

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
func (Executor) Execute(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (Result, error) {
	return execute(ctx, nodeID, node, inputs, nil)
}

func execute(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte)) (Result, error) {
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

	cmd := exec.CommandContext(ctx, cfg.command, cfg.args...)
	cmd.Dir = cfg.cwd
	cmd.Env = append(buildEnvironment(cfg), inputEnvironment...)
	// A descendant may inherit stdout or stderr after the direct child exits.
	// Bound the pipe drain so Wait can finish without managing the process tree.
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &logWriter{capture: &stdout, stream: "stdout", observe: observe}
	cmd.Stderr = &logWriter{capture: &stderr, stream: "stderr", observe: observe}
	var killRequested atomic.Bool
	cmd.Cancel = func() error {
		err := cmd.Process.Kill()
		if err == nil {
			killRequested.Store(true)
		}
		return err
	}
	if err := cmd.Start(); err != nil {
		kind := FailureStart
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			kind = FailureCanceled
		}
		return result, &ExecutionError{NodeID: nodeID, Kind: kind, Result: result, Cause: err}
	}
	result.StartedAt = time.Now()
	err = cmd.Wait()
	result.EndedAt = time.Now()
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		kind := FailureExit
		if errors.Is(err, exec.ErrWaitDelay) {
			kind = FailureOutput
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			kind = FailureCanceled
		}
		if killRequested.Load() && ctx.Err() != nil && cmd.ProcessState != nil {
			// Unix wait status distinguishes a signal kill from a normal
			// nonzero exit that raced with cancellation. Other platforms
			// retain the successful kill request as the available evidence.
			status, hasSignalStatus := cmd.ProcessState.Sys().(interface{ Signaled() bool })
			if runtime.GOOS == "windows" || !hasSignalStatus || status.Signaled() {
				kind, err = FailureCanceled, ctx.Err()
			}
		}
		return result, &ExecutionError{NodeID: nodeID, Kind: kind, Result: result, Cause: err}
	}
	return result, nil
}

// ExecuteNode adapts process output to explicitly declared node output ports.
// A failed process returns no accepted outputs; its evidence is in the error.
func (e Executor) ExecuteNode(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value) (map[string]graph.Value, error) {
	return e.ExecuteNodeWithLogs(ctx, nodeID, node, inputs, nil)
}

// ExecuteNodeWithLogs streams bounded chunks while retaining F0.5's complete
// stdout and stderr result evidence. The observer must tolerate concurrent
// calls from the two output-copy goroutines.
func (Executor) ExecuteNodeWithLogs(ctx context.Context, nodeID string, node graph.NodeDefinition, inputs map[string]graph.Value, observe func(string, []byte)) (map[string]graph.Value, error) {
	cfg, configErr := parseConfig(node)
	if configErr != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: Result{ExitCode: -1}, Cause: configErr}
	}
	if configErr := validateOutputs(node, cfg); configErr != nil {
		return nil, &ExecutionError{NodeID: nodeID, Kind: FailureConfig, Result: Result{ExitCode: -1}, Cause: configErr}
	}
	names := make([]string, 0, len(node.Outputs))
	for name := range node.Outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	result, err := execute(ctx, nodeID, node, inputs, observe)
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

// ValidateNodeConfig performs the same configuration/output validation as
// execution without starting the configured command.
func (Executor) ValidateNodeConfig(_ string, node graph.NodeDefinition) error {
	cfg, err := parseConfig(node)
	if err != nil {
		return err
	}
	return validateOutputs(node, cfg)
}

func validateOutputs(node graph.NodeDefinition, cfg config) error {
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
				return errors.New("json output requires stdout_json: true")
			}
		default:
			return fmt.Errorf("unsupported shell output %q", name)
		}
	}
	return nil
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
	for _, key := range keys {
		switch key {
		case "command", "args", "cwd", "env_allowlist", "env", "input_env", "stdout_json":
		default:
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
