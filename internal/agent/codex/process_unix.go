//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// Every invocation owns a fresh process group, including bounded probes.
// Deliberately escaped process groups require stronger isolation than F1.2.
func runProcess(ctx context.Context, binary string, args, env []string, dir string, stdin io.Reader, stdout io.Writer) error {
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fail("start", "cannot start Codex CLI", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	signal := func(sig syscall.Signal) error {
		err := syscall.Kill(-cmd.Process.Pid, sig)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	cleanup := func() error {
		if errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH) {
			return nil
		}
		term := signal(syscall.SIGTERM)
		timer := time.NewTimer(500 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		return errors.Join(term, signal(syscall.SIGKILL))
	}
	select {
	case err := <-done:
		shutdown := cleanup()
		if shutdown != nil {
			return fail("shutdown", "cannot terminate owned process group", shutdown)
		}
		if errors.Is(err, exec.ErrWaitDelay) {
			return nil
		} // leader succeeded; inherited pipes were bounded
		return err
	case <-ctx.Done():
		shutdown := cleanup()
		select {
		case <-done:
		case <-time.After(time.Second):
			shutdown = errors.Join(shutdown, errors.New("process reap timed out"))
		}
		if shutdown != nil {
			return fail("shutdown", "cancelled; process shutdown failed", errors.Join(ctx.Err(), shutdown))
		}
		return errors.Join(ctx.Err(), context.Cause(ctx))
	}
}
