//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"agentplane/internal/agent"
	"agentplane/internal/proctree"
)

// roadmap: F1.2-AC3
func TestOwnedChildCancellationAndOrdinaryExit(t *testing.T) {
	for _, cancelRun := range []bool{true, false} {
		t.Run(strconv.FormatBool(cancelRun), func(t *testing.T) {
			d := repo(t)
			r := t.TempDir()
			fifo := filepath.Join(r, "ready")
			if e := syscall.Mkfifo(fifo, 0600); e != nil {
				t.Fatal(e)
			}
			t.Setenv("READY", fifo)
			t.Setenv("PID_FILE", filepath.Join(r, "pid"))
			body := `/bin/sh -c 'trap "" TERM; echo $$ >"$PID_FILE"; printf ready >"$READY"; while :; do /bin/sleep 1; done' &
`
			if cancelRun {
				body += "wait\n"
			} else {
				body += `printf '{"summary":"ok","artifacts":[]}' >"$result"`
			}
			a := Adapter{Binary: fake(t, body), Termination: proctree.Policy{Grace: 200 * time.Millisecond}}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ready := make(chan error, 1)
			go func() {
				f, e := os.Open(fifo)
				if e == nil {
					b := make([]byte, 5)
					_, e = f.Read(b)
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
				t.Fatal("child readiness timed out")
			}
			if cancelRun {
				cancel()
			}
			select {
			case e := <-done:
				if cancelRun && !errors.Is(e, context.Canceled) {
					t.Fatalf("cancel=%v", e)
				}
				if !cancelRun && e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown exceeded bound")
			}
			b, e := os.ReadFile(filepath.Join(r, "pid"))
			if e != nil {
				t.Fatal(e)
			}
			pid, e := strconv.Atoi(strings.TrimSpace(string(b)))
			if e != nil {
				t.Fatal(e)
			}
			// A killed descendant may briefly remain a zombie awaiting init reaping;
			// it cannot execute. On Darwin it is normally reaped before this check.
			if err := ownedChildTerminationError(pid, processState); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func processState(pid int) (string, error) {
	b, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(b)), err
}

func ownedChildTerminationError(pid int, stateOf func(int) (string, error)) error {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	} else if err != nil {
		return fmt.Errorf("probe owned child pid=%d: %w", pid, err)
	}
	state, queryErr := stateOf(pid)
	if queryErr == nil && strings.HasPrefix(state, "Z") {
		return nil
	}
	// The child can be reaped after the first existence probe but before ps.
	// Empty state alone is not evidence of termination: ps may have failed.
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	} else if err != nil {
		return fmt.Errorf("recheck owned child pid=%d: %w", pid, errors.Join(err, queryErr))
	}
	if queryErr != nil {
		return fmt.Errorf("cannot verify owned child termination: pid=%d state=%q: %w", pid, state, queryErr)
	}
	return fmt.Errorf("owned child still running: pid=%d state=%q", pid, state)
}

func TestOwnedChildReapedBetweenTerminationObservations(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	})
	queried := false
	err := ownedChildTerminationError(child.Process.Pid, func(pid int) (string, error) {
		queried = true
		// The helper's successful kill(0) precedes this callback. Reap here to
		// deterministically exercise disappearance before the state observation.
		if err := child.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = child.Wait()
		reaped = true
		return processState(pid)
	})
	if !queried {
		t.Fatal("fixture did not reach the two-observation window")
	}
	if err != nil {
		t.Fatalf("reaped owned child falsely reported live: %v", err)
	}
}

func TestInitiallyIgnoredSpecialEntryIsNotRead(t *testing.T) {
	d := repo(t)
	os.WriteFile(filepath.Join(d, ".gitignore"), []byte("ignored-pipe\n"), 0600)
	if err := syscall.Mkfifo(filepath.Join(d, "ignored-pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	a := Adapter{Binary: fake(t, `printf '{"summary":"ok","artifacts":[]}' >"$result"`)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := a.Execute(ctx, agent.AgentExecutionInput{Workspace: d, Task: "no edit"})
	if err != nil || len(out.ChangedFiles) != 0 {
		t.Fatalf("out=%+v error=%v", out, err)
	}
}

func TestOwnedChildTerminationObservationFailsClosedForLiveProcess(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	queryFailure := errors.New("process state observation failed")
	for _, tc := range []struct {
		name, state string
		queryErr    error
	}{
		{name: "running", state: "S"},
		{name: "unknown empty state"},
		{name: "state query failure", queryErr: queryFailure},
		{name: "untrusted partial zombie output", state: "Z", queryErr: queryFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ownedChildTerminationError(child.Process.Pid, func(int) (string, error) { return tc.state, tc.queryErr })
			if err == nil {
				t.Fatal("live or unknown process was accepted as terminated")
			}
			if tc.queryErr != nil && !errors.Is(err, tc.queryErr) {
				t.Fatalf("ps observation cause lost: %v", err)
			}
		})
	}
	t.Run("missing ps", func(t *testing.T) {
		err := ownedChildTerminationError(child.Process.Pid, func(pid int) (string, error) {
			b, err := exec.Command("/missing/agentplane-ps", "-p", strconv.Itoa(pid)).Output()
			return strings.TrimSpace(string(b)), err
		})
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing ps was ignored or misclassified: %v", err)
		}
	})
}
