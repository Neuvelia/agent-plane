//go:build darwin || linux

package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/process"
)

func TestSupervisedShellDoesNotLabelUnverifiedCleanupCanceled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "pid")
	script := filepath.Join(t.TempDir(), "stubborn-supervisor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$$\" > '"+marker+"'\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unusedReady := filepath.Join(t.TempDir(), "never")
	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		_, err := (Executor{SupervisorBinary: script}).Execute(ctx, "worker", helperNode("wait", unusedReady), nil)
		done <- result{err}
	}()
	deadline := time.After(8 * time.Second)
	var pid int
	for pid == 0 {
		if raw, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(raw))
		}
		select {
		case <-deadline:
			t.Fatal("stubborn helper did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case got := <-done:
		var typed *ExecutionError
		var join *process.JoinFailure
		if !errors.As(got.err, &typed) || typed.Kind != FailureCleanup || !errors.As(got.err, &join) || errors.Is(got.err, context.Canceled) {
			t.Fatalf("unverified cleanup = %v", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("join failure did not return")
	}
}

func TestSupervisedShellMissingStatusIsCleanupFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "silent-supervisor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := (Executor{SupervisorBinary: script}).Execute(context.Background(), "worker", helperNode("echo", "unused"), nil)
	var typed *ExecutionError
	var supervisor *process.SupervisorFailure
	if !errors.As(err, &typed) || typed.Kind != FailureCleanup || !errors.As(err, &supervisor) {
		t.Fatalf("missing supervisor status classified as %v", err)
	}
}
