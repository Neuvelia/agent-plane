//go:build darwin || linux

package gate

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

	"agentplane/internal/domain/events"
	"agentplane/internal/proctree"
	"agentplane/internal/runtime"
)

// blockingGate starts a Gate whose fake diff spawns a background grandchild,
// reports the grandchild's pid through a FIFO and then blocks. The FIFO is the
// synchronization point: the test proceeds only once the tree exists.
type blockingGate struct {
	pid    int
	done   chan error
	checks chan events.GateCheckPayload
}

func startBlockingGate(t *testing.T, ctx context.Context, e Executor) *blockingGate {
	t.Helper()
	n := diffNode(t)
	cwd := n.Config["cwd"].(string)
	for _, name := range []string{"left", "right"} {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pid")
	makeFIFO(t, fifo)
	script := filepath.Join(dir, "diff")
	body := "#!/bin/sh\n/bin/sleep 300 &\necho $! > " + fifo + "\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	e.diffPath = script

	g := &blockingGate{done: make(chan error, 1), checks: make(chan events.GateCheckPayload, 1)}
	go func() {
		_, _, err := e.ExecuteNodeObserved(ctx, "gate", n, paths(), runtime.ExecutionObservation{Check: func(p events.GateCheckPayload) { g.checks <- p }})
		g.done <- err
	}()
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
			t.Fatalf("fake diff reported pid %q", s)
		}
		g.pid = pid
	case <-time.After(10 * time.Second):
		t.Fatal("fake diff never started its grandchild")
	}
	t.Cleanup(func() { _ = syscall.Kill(g.pid, syscall.SIGKILL) })
	return g
}

func (g *blockingGate) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-g.done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("gate did not return")
		return nil
	}
}

// alive ignores zombies: see testenv.ProcessAlive.
func alive(pid int) bool { return testenv.ProcessAlive(pid) }

func TestCommandGateKillsDiffTreeOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := startBlockingGate(t, ctx, Executor{Termination: proctree.Policy{Grace: 200 * time.Millisecond}})
	cancel()
	if err := g.wait(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled gate returned %v", err)
	}
	if alive(g.pid) {
		t.Fatalf("grandchild %d survived cancellation", g.pid)
	}
	select {
	case p := <-g.checks:
		t.Fatalf("cancelled gate emitted a verdict: %+v", p)
	default:
	}
}

func TestCommandGateTimeoutIsNotARedVerdict(t *testing.T) {
	g := startBlockingGate(t, context.Background(), Executor{EvidenceBound: 100 * time.Millisecond, Termination: proctree.Policy{Grace: 200 * time.Millisecond}})
	err := g.wait(t)
	var red RedError
	if err == nil || errors.As(err, &red) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out gate returned %v, want an operational deadline error", err)
	}
	if alive(g.pid) {
		t.Fatalf("grandchild %d survived the evidence bound", g.pid)
	}
	select {
	case p := <-g.checks:
		t.Fatalf("timed-out gate emitted a verdict: %+v", p)
	default:
	}
}
