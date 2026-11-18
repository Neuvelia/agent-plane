//go:build unix

package proctree

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const pollInterval = 5 * time.Millisecond

// owner is the Unix ownership state: the group id equals the leader pid.
type owner struct {
	pgid int
	// gone latches once the group was observed empty, so a recycled group id
	// is never signalled afterwards.
	gone bool
	kill func(pid int, sig syscall.Signal) error
}

func (t *Tree) attach(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cmd.SysProcAttr.Pgid != 0 {
		return errors.New("proctree: SysProcAttr.Pgid must be zero; the tree owns a fresh group")
	}
	cmd.SysProcAttr.Setpgid = true
	t.kill = syscall.Kill
	return nil
}

func (t *Tree) started() { t.pgid = t.cmd.Process.Pid }

func (t *Tree) groupEmpty() bool {
	if t.gone {
		return true
	}
	if t.leaderReaped() && t.kill(t.pgid, 0) == nil {
		// Our leader is reaped, so a live process with its pid is a stranger
		// and a group with that id is not ours.
		t.gone = true
		return true
	}
	if errors.Is(t.kill(-t.pgid, 0), syscall.ESRCH) {
		t.gone = true
	}
	return t.gone
}

func (t *Tree) leaderReaped() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Tree) signal(sig syscall.Signal) error {
	if t.groupEmpty() {
		return nil
	}
	if err := t.kill(-t.pgid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signalling process group with %s: %w", sig, err)
	}
	return nil
}

// waitEmpty polls until the group is empty, limit passes, ctx ends or
// escalate closes. It reports whether the group is empty.
func (t *Tree) waitEmpty(ctx context.Context, limit time.Duration, escalate <-chan struct{}) bool {
	timer := time.NewTimer(limit)
	defer timer.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	exited := t.done
	for {
		if t.groupEmpty() {
			return true
		}
		select {
		case <-exited:
			exited = nil
		case <-tick.C:
		case <-timer.C:
			return t.groupEmpty()
		case <-ctx.Done():
			return t.groupEmpty()
		case <-escalate:
			return t.groupEmpty()
		}
	}
}

func (t *Tree) terminate(ctx context.Context, p Policy) (Outcome, error) {
	out := Outcome{Grace: p.grace()}
	start := time.Now()
	if t.groupEmpty() {
		out.Method = MethodAlreadyExited
		return out, nil
	}
	sigErr := t.signal(syscall.SIGTERM)
	if t.waitEmpty(ctx, out.Grace, p.Escalate) {
		out.Method = MethodGraceful
		out.Elapsed = time.Since(start)
		return out, sigErr
	}
	out.Method = MethodForced
	sigErr = errors.Join(sigErr, t.signal(syscall.SIGKILL))
	empty := t.waitEmpty(context.Background(), verifyBound, nil)
	out.Elapsed = time.Since(start)
	if !empty {
		out.Leaked = max(1, countGroup(t.pgid))
		return out, errors.Join(sigErr, &LeakError{Leaked: out.Leaked})
	}
	return out, sigErr
}
