//go:build darwin || linux

package gitstate

import (
	"agentplane/internal/testenv"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeGit installs a git replacement that spawns a background grandchild,
// reports its pid through a FIFO and blocks. The FIFO synchronizes the test
// with the existence of the tree; there is no sleep.
func fakeGit(t *testing.T) (fifo string) {
	t.Helper()
	dir := t.TempDir()
	fifo = filepath.Join(dir, "pid")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "git")
	body := "#!/bin/sh\n/bin/sleep 300 &\necho $! > " + fifo + "\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := resolveGit
	resolveGit = func() (string, error) { return script, nil }
	t.Cleanup(func() { resolveGit = previous })
	return fifo
}

func readPID(t *testing.T, fifo string) int {
	t.Helper()
	read := make(chan string, 1)
	go func() {
		b, err := os.ReadFile(fifo)
		if err != nil {
			read <- ""
			return
		}
		read <- strings.TrimSpace(string(b))
	}()
	select {
	case s := <-read:
		pid, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("fake git reported pid %q", s)
		}
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
		return pid
	case <-time.After(10 * time.Second):
		t.Fatal("fake git never started its grandchild")
		return 0
	}
}

// alive ignores zombies: see testenv.ProcessAlive.
func alive(pid int) bool { return testenv.ProcessAlive(pid) }

func runFake(ctx context.Context, opts RunOptions) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, os.TempDir(), opts, "status")
		done <- err
	}()
	return done
}

func awaitRun(t *testing.T, done <-chan error) *RunError {
	t.Helper()
	select {
	case err := <-done:
		var re *RunError
		if !errors.As(err, &re) {
			t.Fatalf("Run returned %T %v, want *RunError", err, err)
		}
		return re
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func TestGitRunTimeoutKillsGrandchild(t *testing.T) {
	fifo := fakeGit(t)
	done := runFake(context.Background(), RunOptions{Timeout: 2 * time.Second})
	pid := readPID(t, fifo)
	if re := awaitRun(t, done); re.Class != RunTimeout {
		t.Fatalf("class = %s, want %s", re.Class, RunTimeout)
	}
	if alive(pid) {
		t.Fatalf("grandchild %d survived the time bound", pid)
	}
}

func TestGitRunCancelKillsGrandchildAndKeepsClass(t *testing.T) {
	fifo := fakeGit(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runFake(ctx, RunOptions{})
	pid := readPID(t, fifo)
	cancel()
	re := awaitRun(t, done)
	if re.Class != RunCancelled || !errors.Is(re, context.Canceled) {
		t.Fatalf("got class %s: %v, want %s wrapping context.Canceled", re.Class, re, RunCancelled)
	}
	if alive(pid) {
		t.Fatalf("grandchild %d survived cancellation", pid)
	}
}
