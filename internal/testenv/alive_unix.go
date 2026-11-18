//go:build unix

package testenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ProcessAlive reports whether pid can still run. kill(pid, 0) succeeds for a
// zombie, a process that has exited but whose parent (or an init that does not
// reap orphans, as in many containers) has not collected it yet; a zombie cannot
// run, so it is not alive here. Tests that assert "the grandchild is gone" must
// use this instead of kill(pid, 0) or they fail wherever reaping is slow.
func ProcessAlive(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return false
	}
	return !isZombie(pid)
}

// isZombie reads the state from /proc where it exists and from ps elsewhere
// (macOS). A failed lookup is not evidence of a zombie.
func isZombie(pid int) bool {
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		stat := string(data)
		// the command name is parenthesized and may contain spaces; the state
		// is the first field after the last ')'.
		i := strings.LastIndexByte(stat, ')')
		return i >= 0 && strings.HasPrefix(strings.TrimSpace(stat[i+1:]), "Z")
	}
	out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}
