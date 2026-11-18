//go:build linux

package proctree

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestStartKeepsCallerSysProcAttr(t *testing.T) {
	cmd := helperCmd("hold")
	// Setsid cannot combine with Setpgid, so Pdeathsig (harmless SIGCONT)
	// stands for any field a Prepare hook sets.
	tree, r := startCmd(t, cmd, func(c *exec.Cmd) error {
		c.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGCONT}
		return nil
	})
	readReadyPID(t, r)
	attr := cmd.SysProcAttr
	if attr.Pdeathsig != syscall.SIGCONT || !attr.Setpgid {
		t.Fatalf("SysProcAttr = %+v; want the hook's Pdeathsig merged with Setpgid", attr)
	}
	if _, err := tree.Terminate(context.Background(), Policy{Grace: testGrace}); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxCountsGroupMembers(t *testing.T) {
	tree, r := startHelper(t, "spawn")
	readPIDs(t, r)
	if n := countGroup(tree.pgid); n != 2 {
		t.Fatalf("countGroup = %d; want leader and grandchild", n)
	}
	if n := countGroup(os.Getpid() + 1<<22); n != 0 {
		t.Fatalf("countGroup of an unused group = %d; want 0", n)
	}
}

func TestStatGroupParsesCommandNamesWithParentheses(t *testing.T) {
	stat := "123 (a) b (c)) S 1 456 456 0 -1"
	if got := statGroup(stat); got != 456 {
		t.Fatalf("statGroup = %d; want 456", got)
	}
}
