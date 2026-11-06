//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/process"
)

func TestSupervisedCodexDoesNotLabelUnverifiedCleanupCanceled(t *testing.T) {
	dir := repo(t)
	marker := filepath.Join(t.TempDir(), "pid")
	script := filepath.Join(t.TempDir(), "stubborn-supervisor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$$\" > '"+marker+"'\nprintf '{\"type\":\"error\"}\\n'\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	binary := fake(t, "exit 0")
	done := make(chan error, 1)
	go func() {
		_, err := (Adapter{Binary: binary, SupervisorBinary: script}).Execute(ctx, agent.AgentExecutionInput{Workspace: dir, Task: "work"})
		done <- err
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
	case err := <-done:
		var typed *agent.ExecutionError
		var join *process.JoinFailure
		if !errors.As(err, &typed) || typed.Kind != "codex_cleanup" || !errors.As(err, &join) || errors.Is(err, context.Canceled) {
			t.Fatalf("unverified cleanup = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("join failure did not return")
	}
}

func TestSupervisedCodexMissingStatusIsCleanupFailure(t *testing.T) {
	dir := repo(t)
	script := filepath.Join(t.TempDir(), "silent-supervisor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := (Adapter{Binary: fake(t, "exit 0"), SupervisorBinary: script}).Execute(context.Background(), agent.AgentExecutionInput{Workspace: dir, Task: "work"})
	var typed *agent.ExecutionError
	var supervisor *process.SupervisorFailure
	if !errors.As(err, &typed) || typed.Kind != "codex_cleanup" || !errors.As(err, &supervisor) {
		t.Fatalf("missing supervisor status classified as %v", err)
	}
}
