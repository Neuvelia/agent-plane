//go:build !windows

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/proctree"
)

// cancelWithGrandchild runs a fake CLI whose same-group descendant writes its
// PID, cancels once it is ready, and returns the execution error and that PID.
func cancelWithGrandchild(t *testing.T, a Adapter, descendant string) (int, error) {
	t.Helper()
	d := repo(t)
	r := t.TempDir()
	fifo := filepath.Join(r, "ready")
	if e := syscall.Mkfifo(fifo, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("READY", fifo)
	t.Setenv("PID_FILE", filepath.Join(r, "pid"))
	a.Binary = fake(t, descendant+" &\nwait\n")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ready := make(chan error, 1)
	go func() {
		f, e := os.Open(fifo)
		if e == nil {
			_, e = f.Read(make([]byte, 5))
			f.Close()
		}
		ready <- e
	}()
	done := make(chan error, 1)
	go func() {
		_, e := a.Execute(ctx, agent.AgentExecutionInput{Workspace: d, Task: "work", EnvironmentRefs: []string{"READY", "PID_FILE"}})
		done <- e
	}()
	select {
	case e := <-ready:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("grandchild readiness timed out")
	}
	cancel()
	var execErr error
	select {
	case execErr = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("execution did not finish after cancel")
	}
	b, e := os.ReadFile(filepath.Join(r, "pid"))
	if e != nil {
		t.Fatal(e)
	}
	pid, e := strconv.Atoi(strings.TrimSpace(string(b)))
	if e != nil {
		t.Fatal(e)
	}
	return pid, execErr
}

func terminationOf(t *testing.T, err error) *proctree.Outcome {
	t.Helper()
	var p interface{ ProcessTermination() *proctree.Outcome }
	if !errors.As(err, &p) {
		t.Fatalf("error carries no process termination record: %v", err)
	}
	o := p.ProcessTermination()
	if o == nil {
		t.Fatalf("process termination record is nil: %v", err)
	}
	return o
}

func TestCancelledExecutionCarriesGracefulTermination(t *testing.T) {
	pid, err := cancelWithGrandchild(t, Adapter{}, `/bin/sh -c 'echo $$ >"$PID_FILE"; printf ready >"$READY"; while :; do /bin/sleep 1; done'`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	var typed *agent.ExecutionError
	if !errors.As(err, &typed) || typed.DiagnosticCode() != "codex_cancelled" {
		t.Fatalf("want typed codex_cancelled, got %v", err)
	}
	o := terminationOf(t, err)
	if o.Method != proctree.MethodGraceful || o.Grace != proctree.DefaultGrace {
		t.Fatalf("outcome=%+v", *o)
	}
	if e := ownedChildTerminationError(pid, processState); e != nil {
		t.Fatal(e)
	}
}

func TestCodexForcedKillAfterGraceWithDescendant(t *testing.T) {
	a := Adapter{Termination: proctree.Policy{Grace: 200 * time.Millisecond}}
	pid, err := cancelWithGrandchild(t, a, `/bin/sh -c 'trap "" TERM; echo $$ >"$PID_FILE"; printf ready >"$READY"; while :; do /bin/sleep 1; done'`)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	o := terminationOf(t, err)
	if o.Method != proctree.MethodForced || o.Grace != 200*time.Millisecond {
		t.Fatalf("outcome=%+v", *o)
	}
	if e := ownedChildTerminationError(pid, processState); e != nil {
		t.Fatal(e)
	}
}
