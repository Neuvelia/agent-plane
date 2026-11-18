//go:build unix

package proctree

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func killPID(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

func init() {
	// setsid-child: start a "hold" descendant in its own session, so it leaves
	// the tree's process group, then report both pids.
	extraModes["setsid-child"] = func([]string) {
		child := helperCmd("hold")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		out, err := child.StdoutPipe()
		if err != nil {
			os.Exit(91)
		}
		if err := child.Start(); err != nil {
			os.Exit(92)
		}
		if _, err := bufio.NewReader(out).ReadString('\n'); err != nil {
			os.Exit(93)
		}
		fmt.Printf("pids %d %d\n", os.Getpid(), child.Process.Pid)
		block()
	}
}

// Motivation: killing only the leader, as exec.CommandContext does, leaves the
// grandchild running. This is the behavior proctree replaces.
func TestPlainLeaderKillLeavesGrandchild(t *testing.T) {
	cmd := helperCmd("spawn")
	cmd.Stdout = nil
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(out)
	_, child := readPIDs(t, r)
	t.Cleanup(func() { killPID(child) })
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if !alive(child) {
		t.Fatal("grandchild died with the leader; the motivating failure no longer reproduces")
	}
}

func TestTerminateGracefulWithinGrace(t *testing.T) {
	tree, r := startHelper(t, "exit-on-term")
	readReadyPID(t, r)
	out, err := tree.Terminate(context.Background(), Policy{Grace: testGrace})
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != MethodGraceful || out.Elapsed >= testGrace || out.Grace != testGrace {
		t.Fatalf("outcome = %+v; want graceful within %s", out, testGrace)
	}
}

func TestTerminateForcedAfterGrace(t *testing.T) {
	const grace = 100 * time.Millisecond
	tree, r := startHelper(t, "ignore-term")
	pid := readReadyPID(t, r)
	out, err := tree.Terminate(context.Background(), Policy{Grace: grace})
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != MethodForced || out.Elapsed < grace {
		t.Fatalf("outcome = %+v; want forced after %s", out, grace)
	}
	waitGone(t, pid)
}

func TestEscalateSkipsRemainingGrace(t *testing.T) {
	tree, r := startHelper(t, "ignore-term")
	pid := readReadyPID(t, r)
	escalate := make(chan struct{})
	close(escalate)
	out, err := tree.Terminate(context.Background(), Policy{Grace: 30 * time.Second, Escalate: escalate})
	if err != nil {
		t.Fatal(err)
	}
	if out.Method != MethodForced || out.Elapsed >= verifyBound+time.Second {
		t.Fatalf("outcome = %+v; want forced well under the 30s grace", out)
	}
	waitGone(t, pid)
}

func TestCancelledContextShortensGrace(t *testing.T) {
	tree, r := startHelper(t, "ignore-term")
	pid := readReadyPID(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := tree.Terminate(ctx, Policy{Grace: 30 * time.Second})
	if err != nil || out.Method != MethodForced {
		t.Fatalf("outcome = %+v, %v; want forced", out, err)
	}
	waitGone(t, pid)
}

func TestLeakedDescendantYieldsLeakError(t *testing.T) {
	tree, r := startHelper(t, "hold")
	pid := readReadyPID(t, r)
	// A member that SIGKILL cannot remove is not constructible with real
	// processes, so the signalling syscall drops SIGTERM and SIGKILL.
	realKill := tree.kill
	tree.kill = func(p int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL || sig == syscall.SIGTERM {
			return nil
		}
		return realKill(p, sig)
	}
	t.Cleanup(func() { realKill(-tree.pgid, syscall.SIGKILL) })
	out, err := tree.Terminate(context.Background(), Policy{Grace: 50 * time.Millisecond})
	var leak *LeakError
	if !errors.As(err, &leak) || leak.Leaked < 1 || out.Leaked != leak.Leaked || out.Method != MethodForced {
		t.Fatalf("Terminate = %+v, %v; want forced with *LeakError", out, err)
	}
	if !alive(pid) {
		t.Fatal("test setup: leader should still be alive")
	}
}

func TestEscapedSessionIsOutOfScopeAndNotSignalled(t *testing.T) {
	tree, r := startHelper(t, "setsid-child")
	leader, escaped := readPIDs(t, r)
	t.Cleanup(func() { killPID(escaped) })
	out, err := tree.Terminate(context.Background(), Policy{Grace: testGrace})
	if err != nil {
		t.Fatalf("Terminate: %v (an escaped session is outside the owned group)", err)
	}
	waitGone(t, leader)
	if !alive(escaped) {
		t.Fatalf("a process in another session was killed: outcome %+v", out)
	}
}

func TestSignalsOnlyTheOwnedGroup(t *testing.T) {
	sibling := helperCmd("hold")
	sibling.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	_, sr := startCmd(t, sibling, nil)
	siblingPID := readReadyPID(t, sr)
	t.Cleanup(func() { killPID(siblingPID) })

	tree, r := startHelper(t, "ignore-term")
	readReadyPID(t, r)
	if _, err := tree.Terminate(context.Background(), Policy{Grace: 50 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if !alive(siblingPID) {
		t.Fatal("a process in a different group was signalled")
	}
}

func TestStartMakesTheLeaderItsOwnGroup(t *testing.T) {
	tree, r := startHelper(t, "hold")
	pid := readReadyPID(t, r)
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid {
		t.Fatalf("pgid = %d, %v; want leader pid %d", pgid, err, pid)
	}
	if tree.pgid != pid {
		t.Fatalf("tree group %d != pid %d", tree.pgid, pid)
	}
}

func TestPrepareCannotOverwriteProctreeFields(t *testing.T) {
	cmd := helperCmd("hold")
	tree, r := startCmd(t, cmd, func(c *exec.Cmd) error {
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: false}
		return nil
	})
	pid := readReadyPID(t, r)
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Fatalf("pgid = %d, %v; want own group %d", pgid, err, pid)
	}
	if _, err := tree.Terminate(context.Background(), Policy{Grace: testGrace}); err != nil {
		t.Fatal(err)
	}
	waitGone(t, pid)
}

func TestStartRejectsForeignProcessGroup(t *testing.T) {
	cmd := helperCmd("hold")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pgid: 1}
	if tree, err := Start(cmd); err == nil {
		tree.Terminate(context.Background(), Policy{Grace: time.Millisecond})
		t.Fatal("Start accepted a preset Pgid")
	}
}
