//go:build darwin || linux

package codex

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
)

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
			a := Adapter{Binary: fake(t, body)}
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
			if e = syscall.Kill(pid, 0); e == nil {
				state := processState(pid)
				if !strings.HasPrefix(state, "Z") {
					t.Fatalf("owned child still running: pid=%d state=%q", pid, state)
				}
			}
		})
	}
}

func processState(pid int) string {
	b, _ := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(b))
}
