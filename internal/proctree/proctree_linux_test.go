//go:build linux

package proctree

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
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

// A group whose only members are zombies is empty: the zombie cannot run, and
// kill(-pgid, 0) keeps succeeding until its parent or an init reaps it, which in
// some containers is slow or never. The test is the zombie's parent, so it stays
// a zombie until the test waits for it, whatever the machine's init does.
func TestGroupOfOnlyZombiesIsEmpty(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	pgid := cmd.Process.Pid
	// the exit is asynchronous: wait (bounded) until nothing in the group can run
	deadline := time.Now().Add(10 * time.Second)
	for countLiveGroup(pgid) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the child never became a zombie")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if countGroup(pgid) != 1 || syscall.Kill(-pgid, 0) != nil {
		t.Fatalf("fixture is not a zombie-only group: members=%d", countGroup(pgid))
	}
	tree := &Tree{done: make(chan struct{})} // the leader is not reaped, as from the tree's view
	tree.pgid, tree.kill = pgid, syscall.Kill
	if !tree.groupEmpty() {
		t.Fatal("a group of only zombies must count as empty")
	}
}

func TestGroupWithALiveMemberIsNotEmpty(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "read x")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() })
	tree := &Tree{done: make(chan struct{})}
	tree.pgid, tree.kill = cmd.Process.Pid, syscall.Kill
	if tree.groupEmpty() {
		t.Fatal("a group with a running process must not count as empty")
	}
}

func TestStatZombieReadsTheStateAfterTheCommandName(t *testing.T) {
	for stat, want := range map[string]bool{
		"123 (sh) Z 1 123 123 0 -1":      true,
		"123 (sh) S 1 123 123 0 -1":      false,
		"123 (a) Z (b) R 1 123 123 0 -1": false, // parentheses and a 'Z' inside the name
		"123 (weird) name) Z 1 5 5":      true,
		"garbage":                        false,
	} {
		if got := statZombie(stat); got != want {
			t.Errorf("statZombie(%q) = %v, want %v", stat, got, want)
		}
	}
}
