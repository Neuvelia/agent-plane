//go:build windows

package proctree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Interim implementation: no Job Object yet (F3.4a), so only the direct
// process is terminated and descendants are not owned.
type owner struct{}

func (t *Tree) attach(*exec.Cmd) error { return nil }

func (t *Tree) started() {}

func (t *Tree) terminate(ctx context.Context, p Policy) (Outcome, error) {
	out := Outcome{Grace: p.grace()}
	start := time.Now()
	select {
	case <-t.done:
		out.Method = MethodAlreadyExited
		return out, nil
	default:
	}
	out.Method = MethodForced
	if err := t.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return out, fmt.Errorf("terminating process: %w", err)
	}
	timer := time.NewTimer(verifyBound)
	defer timer.Stop()
	select {
	case <-t.done:
	case <-timer.C:
		out.Leaked = 1
		out.Elapsed = time.Since(start)
		return out, &LeakError{Leaked: 1}
	}
	out.Elapsed = time.Since(start)
	return out, nil
}
