//go:build !windows

package shell

import (
	"agentplane/internal/proctree"
	"agentplane/internal/testenv"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func init() {
	extraModes["ignore-term"] = func(args []string) {
		signal.Ignore(syscall.SIGTERM)
		writeAtomic(args[1], "ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	extraModes["term-exit0"] = func(args []string) {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM)
		writeAtomic(args[1], "ready")
		<-c
		os.Exit(0)
	}
	// spawn-escaped starts a pipe-holding descendant in its own session, so it
	// leaves the tree's process group.
	extraModes["spawn-escaped"] = func(args []string) {
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "pid-hold", args[1])
		child.Env = os.Environ()
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := child.Start(); err != nil {
			os.Exit(92)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(args[1]); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(93)
			}
			time.Sleep(10 * time.Millisecond)
		}
		writeAtomic(args[2], "ready")
		for {
			time.Sleep(time.Hour)
		}
	}
}

// processAlive ignores zombies: see testenv.ProcessAlive.
func processAlive(pid int) bool { return testenv.ProcessAlive(pid) }

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

func TestExecuteCancellationBoundsDrainForEscapedPipeHolder(t *testing.T) {
	dir := t.TempDir()
	pidFile, ready := filepath.Join(dir, "escaped.pid"), filepath.Join(dir, "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (Executor{}).Execute(ctx, "worker", helperNode("spawn-escaped", pidFile, ready), nil)
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
	case <-time.After(10 * time.Second):
		t.Fatal("execute stayed blocked on an escaped descendant's pipes")
	}
}
