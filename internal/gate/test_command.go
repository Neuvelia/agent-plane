package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentplane/internal/domain/events"
	"agentplane/internal/domain/graph"
	rt "agentplane/internal/runtime"
)

// testCommandBound limits one test command run when Executor.TestBound is zero.
const testCommandBound = 10 * time.Minute

// Declared side-effect policies of a test_command Gate (ADR-0035). They are the
// authors statement, recorded in the Check Result reason; the Gate neither
// enforces nor verifies them.
const (
	SideEffectsNone      = "none"
	SideEffectsWorkspace = "workspace_writes"
)

// reservedAttemptEnv mirrors the Shell node's reserved variable name.
const reservedAttemptEnv = "AGENTPLANE_ATTEMPT"

// validateTestCommand checks the static configuration of a test_command Gate.
// Bound inputs only order the check after the candidate: they never reach the
// command, its arguments, its environment or its cwd.
func validateTestCommand(c config, n graph.NodeDefinition) error {
	if c.Command == "" || strings.ContainsRune(c.Command, 0) {
		return errors.New("test_command requires a nonempty command")
	}
	if !filepath.IsAbs(c.Cwd) || filepath.Clean(c.Cwd) != c.Cwd {
		return errors.New("test_command requires a canonical absolute cwd")
	}
	if c.SideEffects != SideEffectsNone && c.SideEffects != SideEffectsWorkspace {
		return fmt.Errorf("test_command side_effects must be %s or %s", SideEffectsNone, SideEffectsWorkspace)
	}
	for _, name := range c.EnvAllowlist {
		if err := checkEnvName("env_allowlist", name); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(c.Env) {
		if err := checkEnvName("env", name); err != nil {
			return err
		}
		if strings.ContainsRune(c.Env[name], 0) {
			return fmt.Errorf("env %q contains a NUL byte", name)
		}
	}
	candidate, ok := n.Inputs[c.Input]
	if c.Input == "" || !ok || !candidate.Required || (candidate.Type != graph.ValueTypeString && candidate.Type != graph.ValueTypeJSON) {
		return fmt.Errorf("test_command input %q must be a required string or JSON port", c.Input)
	}
	return nil
}

func checkEnvName(field, name string) error {
	if name == "" || strings.ContainsAny(name, "=\x00") {
		return fmt.Errorf("invalid %s name %q", field, name)
	}
	if strings.EqualFold(name, reservedAttemptEnv) {
		return fmt.Errorf("%s %s is reserved for the runtime", field, reservedAttemptEnv)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// testEnvironment is the child's whole environment: nothing is inherited except
// the named allowlist entries that are present, then explicit values, which win.
func testEnvironment(c config) []string {
	values := map[string]string{}
	for _, name := range c.EnvAllowlist {
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	for name, value := range c.Env {
		for existing := range values {
			if strings.EqualFold(existing, name) {
				delete(values, existing)
			}
		}
		values[name] = value
	}
	environment := make([]string, 0, len(values))
	for _, name := range sortedKeys(values) {
		environment = append(environment, name+"="+values[name])
	}
	return environment
}

// testCommand runs the configured command and maps its exit status: 0 is
// green, any other exit is red with bounded, redacted evidence. A start
// failure, signal death, cancellation or timeout is an operational error and
// never a verdict, so a broken or hung runner cannot masquerade as a failing
// test (ADR-0035).
func (e Executor) testCommand(ctx context.Context, c config, o rt.ExecutionObservation) (events.GateCheckPayload, error) {
	var p events.GateCheckPayload
	info, err := os.Stat(c.Cwd)
	if err != nil || !info.IsDir() {
		return p, errors.New("test_command cwd must be an existing directory")
	}
	capture := o.NewCheckCapture
	if capture == nil {
		capture = rt.NewCheckCapture
	}
	stdout, stderr := capture(), capture()
	bound := e.TestBound
	if bound <= 0 {
		bound = testCommandBound
	}
	bounded, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	cmd := exec.Command(c.Command, c.Args...)
	cmd.Dir = c.Cwd
	cmd.Env = testEnvironment(c)
	cmd.WaitDelay = time.Second
	cmd.Stdout, cmd.Stderr = stdout, stderr
	waitErr, err := runOwned(bounded, cmd, e.Termination, "test command")
	out, outOmitted := stdout.Finish()
	errs, errOmitted := stderr.Finish()
	if err != nil {
		if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
			return p, fmt.Errorf("test command exceeded %s: %w", bound, errors.Join(context.DeadlineExceeded, err))
		}
		return p, err
	}
	exit, err := testExit(waitErr)
	if err != nil {
		return p, err
	}
	p = events.GateCheckPayload{CheckID: c.CheckID, Kind: events.CheckKind(c.Kind), Verdict: "green",
		Reason:   fmt.Sprintf("command exited 0; declared side_effects: %s", c.SideEffects),
		Evidence: []events.GateEvidence{{Source: events.EvidenceSource(c.Kind), ExitCode: &exit, Stdout: out, Stderr: errs, StdoutOmitted: outOmitted, StderrOmitted: errOmitted}}}
	if exit != 0 {
		p.Verdict = "red"
		p.Reason = fmt.Sprintf("command exited %d; declared side_effects: %s", exit, c.SideEffects)
	}
	return p, nil
}

// testExit returns the exit status of a leader that exited by itself. A process
// without an exit status (killed by a signal) is an operational error.
func testExit(waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode(), nil
	}
	return 0, fmt.Errorf("test command did not report an exit status: %w", waitErr)
}
