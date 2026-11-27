//go:build darwin || linux

package claudecode

import (
	"context"
	"errors"
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

// roadmap: F1.2b-AC5
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
				body += emit(recResult)
			}
			a := Adapter{Workspaces: testWorkspaces{}, Binary: fake(t, body), Termination: proctree.Policy{Grace: 200 * time.Millisecond}}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
			if err := childGone(pid); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// childGone accepts a vanished process or a zombie awaiting reaping; a zombie
// cannot execute.
func childGone(pid int) error {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	out, queryErr := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if queryErr == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
		return nil
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return errors.New("owned child still running")
}
