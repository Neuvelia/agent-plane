//go:build unix

package testenv

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessAliveSeesARunningProcess(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatal("the test process itself must be alive")
	}
}

func TestProcessAliveTreatsAnExitedUnreapedChildAsGone(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	// Until Wait runs the child is a zombie once it has exited: kill(pid, 0)
	// still succeeds, ProcessAlive must not. The exit is asynchronous, so poll
	// with a bound; no assumption is made about how fast the child ends.
	deadline := time.Now().Add(10 * time.Second)
	for ProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatal("an exited, unreaped child was still reported alive")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if ProcessAlive(pid) {
		t.Fatal("a reaped child was reported alive")
	}
}
