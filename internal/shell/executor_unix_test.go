//go:build unix

package shell

import (
	"agentplane/internal/proctree"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func processAlive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestExecuteCancellationKillsGrandchild(t *testing.T) {
	dir := t.TempDir()
	pidFile, ready := filepath.Join(dir, "grandchild.pid"), filepath.Join(dir, "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (Executor{}).Execute(ctx, "worker", helperNode("spawn-tree", pidFile, ready), nil)
		done <- err
	}()
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("tree ended before cancellation: %v", err)
		case <-deadline:
			t.Fatal("process tree did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	})
	cancel()
	select {
	case err := <-done:
		var executionErr *ExecutionError
		if !errors.As(err, &executionErr) || executionErr.Kind != FailureCanceled {
			t.Fatalf("cancellation = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("execute did not return after cancellation")
	}
	if processAlive(pid) {
		t.Fatalf("grandchild %d survived cancellation", pid)
	}
}

func cancelWhenReady(t *testing.T, executor Executor, mode string) error {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := executor.Execute(ctx, "worker", helperNode(mode, ready), nil)
		done <- err
	}()
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("child ended before cancellation: %v", err)
		case <-deadline:
			t.Fatal("child did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("execute did not return after cancellation")
		return nil
	}
}

func TestExecuteForcedKillRecordsMethod(t *testing.T) {
	err := cancelWhenReady(t, Executor{Policy: proctree.Policy{Grace: 200 * time.Millisecond}}, "ignore-term")
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureCanceled {
		t.Fatalf("cancellation = %v", err)
	}
	if got := executionErr.ProcessTermination(); got == nil || got.Method != proctree.MethodForced {
		t.Fatalf("termination = %#v", got)
	}
}

func TestExecuteExitCodeZeroAfterKillStillCanceled(t *testing.T) {
	err := cancelWhenReady(t, Executor{}, "term-exit0")
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Kind != FailureCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if got := executionErr.ProcessTermination(); got == nil || got.Method != proctree.MethodGraceful {
		t.Fatalf("termination = %#v", got)
	}
}
