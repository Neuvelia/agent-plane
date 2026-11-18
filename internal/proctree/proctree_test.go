package proctree

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"
)

const testGrace = 10 * time.Second // an upper bound for graceful cases, never waited out

func waitGone(t *testing.T, pids ...int) {
	t.Helper()
	for _, pid := range pids {
		if alive(pid) {
			t.Errorf("process %d is still alive after Terminate", pid)
		}
	}
}

func TestTerminateKillsGrandchild(t *testing.T) {
	tree, r := startHelper(t, "spawn")
	leader, child := readPIDs(t, r)
	if !alive(leader) || !alive(child) {
		t.Fatalf("tree not running: leader=%v child=%v", alive(leader), alive(child))
	}
	if _, err := tree.Terminate(context.Background(), Policy{Grace: testGrace}); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	waitGone(t, leader, child)
}

func TestTerminateAlreadyExitedIsSuccess(t *testing.T) {
	tree, _ := startHelper(t, "exit")
	if err := tree.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	out, err := tree.Terminate(context.Background(), Policy{Grace: testGrace})
	if err != nil || out.Method != MethodAlreadyExited {
		t.Fatalf("Terminate = %+v, %v; want already_exited", out, err)
	}
}

func TestTerminateBeforeWaitAndAfterWaitIsIdempotent(t *testing.T) {
	tree, r := startHelper(t, "hold")
	readReadyPID(t, r)
	first, err := tree.Terminate(context.Background(), Policy{Grace: testGrace})
	if err != nil || first.Method == MethodAlreadyExited {
		t.Fatalf("first Terminate = %+v, %v", first, err)
	}
	second, err := tree.Terminate(context.Background(), Policy{Grace: testGrace})
	if err != nil || second.Method != MethodAlreadyExited {
		t.Fatalf("second Terminate = %+v, %v; want already_exited", second, err)
	}
	tree.Wait()
	third, err := tree.Terminate(context.Background(), Policy{Grace: testGrace})
	if err != nil || third.Method != MethodAlreadyExited {
		t.Fatalf("Terminate after Wait = %+v, %v; want already_exited", third, err)
	}
}

func TestTerminateNeverStartedTreeIsSuccess(t *testing.T) {
	var nilTree *Tree
	if out, err := nilTree.Terminate(context.Background(), Policy{}); err != nil || out.Method != MethodAlreadyExited {
		t.Fatalf("nil Tree Terminate = %+v, %v", out, err)
	}
	if err := nilTree.Wait(); err != nil {
		t.Fatalf("nil Tree Wait: %v", err)
	}
	failed, err := Start(exec.Command("agentplane-proctree-no-such-binary"))
	if err == nil {
		t.Fatal("Start of a missing binary succeeded")
	}
	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Fatalf("start error %T must stay *exec.Error", err)
	}
	if out, err := failed.Terminate(context.Background(), Policy{}); err != nil || out.Method != MethodAlreadyExited {
		t.Fatalf("failed Start Terminate = %+v, %v", out, err)
	}
}

func TestStartRejectsAStartedCommand(t *testing.T) {
	tree, r := startHelper(t, "hold")
	readReadyPID(t, r)
	if _, err := Start(tree.cmd); err == nil {
		t.Fatal("Start accepted an already started command")
	}
}

func TestPrepareRunsBeforeStartAndErrorsAreWrapped(t *testing.T) {
	cmd := helperCmd("exit")
	boom := errors.New("boom")
	if _, err := StartWith(cmd, func(*exec.Cmd) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("Prepare error not wrapped: %v", err)
	}
	if cmd.Process != nil {
		t.Fatal("command started despite a Prepare error")
	}
	called := false
	tree, err := StartWith(cmd, func(c *exec.Cmd) error {
		called = c.Process == nil
		return nil
	})
	if err != nil || !called {
		t.Fatalf("StartWith = %v, Prepare saw unstarted cmd = %v", err, called)
	}
	if err := tree.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

// readySignal is a non-*os.File stdout, so exec copies through a goroutine
// that a pipe-holding descendant can keep open.
type readySignal struct {
	ready chan struct{}
	once  sync.Once
}

func (r *readySignal) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("pids")) {
		r.once.Do(func() { close(r.ready) })
	}
	return len(p), nil
}

func TestWaitDrainIsBoundedByWaitDelay(t *testing.T) {
	cmd := helperCmd("spawn-inherit")
	sig := &readySignal{ready: make(chan struct{})}
	cmd.Stdout = sig
	cmd.WaitDelay = 200 * time.Millisecond
	tree, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		tree.Terminate(context.Background(), Policy{Grace: time.Millisecond})
		tree.Wait()
	})
	<-sig.ready
	// Ending only the leader leaves the grandchild holding the stdout pipe,
	// which is what WaitDelay must bound.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { tree.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return within the WaitDelay bound")
	}
}
