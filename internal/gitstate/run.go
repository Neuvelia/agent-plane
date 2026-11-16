package gitstate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// RunClass classifies why a controlled Git invocation failed. It never carries
// Git's own diagnostics: stderr is discarded so repository-controlled text
// cannot reach events or terminals through an error message.
type RunClass string

const (
	RunExit        RunClass = "exit"         // Git ran and returned a non-zero status
	RunTimeout     RunClass = "timeout"      // the per-command time bound elapsed
	RunCancelled   RunClass = "cancelled"    // the caller's context ended
	RunOutputLimit RunClass = "output_limit" // stdout exceeded RunOptions.StdoutLimit
	RunStart       RunClass = "start"        // Git could not be started
)

type writeOnly struct{ w io.Writer }

func (w writeOnly) Write(p []byte) (int, error) { return w.w.Write(p) }

// RunError is the only error type returned by Run.
type RunError struct {
	Class    RunClass
	ExitCode int // -1 unless Class is RunExit
	cause    error
}

func (e *RunError) Error() string {
	if e.Class == RunExit {
		return fmt.Sprintf("git failed with exit status %d", e.ExitCode)
	}
	return "git " + string(e.Class)
}
func (e *RunError) Unwrap() error { return e.cause }

// RunOptions bounds one invocation. Zero values select the defaults used for
// workspace inspection: five seconds and 16 MiB of stdout.
type RunOptions struct {
	Timeout     time.Duration
	StdoutLimit int
}

// RunResult carries the output of a Git process that actually ran. ExitCode is
// meaningful even when Run also returns an error (for example `merge-tree`
// exits 1 and prints the conflicting paths).
type RunResult struct {
	Stdout   []byte
	ExitCode int
}

// Run is the single hardened way this repository starts Git: /usr/bin/git,
// structured argv, a minimal fixed environment (no system or global config, no
// inherited GIT_* variables), hooks and fsmonitor disabled, bounded time and
// stdout, stderr discarded. Callers pass any `-c key=value` they need (for
// example a commit identity) as leading arguments. It does not make the
// repository's own local configuration trustworthy; callers that create
// checkouts must additionally refuse repositories that configure filter
// drivers.
func Run(ctx context.Context, dir string, opts RunOptions, args ...string) (RunResult, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	limit := opts.StdoutLimit
	if limit <= 0 {
		limit = 16 << 20
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/git", argv...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	var out limitedBuffer
	out.limit = limit
	// limitedBuffer embeds bytes.Buffer, whose promoted ReadFrom would let
	// io.Copy bypass the limit; expose only Write to the process.
	cmd.Stdout = writeOnly{&out}
	cmd.Stderr = io.Discard
	err := cmd.Run()
	result := RunResult{Stdout: out.Bytes(), ExitCode: -1}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if out.overflow {
		return result, &RunError{Class: RunOutputLimit, ExitCode: -1, cause: err}
	}
	if err == nil {
		return result, nil
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return result, &RunError{Class: RunTimeout, ExitCode: -1, cause: errors.Join(err, ctx.Err())}
	case ctx.Err() != nil:
		return result, &RunError{Class: RunCancelled, ExitCode: -1, cause: errors.Join(err, ctx.Err())}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return result, &RunError{Class: RunExit, ExitCode: exit.ExitCode(), cause: err}
	}
	return result, &RunError{Class: RunStart, ExitCode: -1, cause: err}
}
