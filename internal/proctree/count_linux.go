//go:build linux

package proctree

import (
	"os"
	"strconv"
	"strings"
)

// countGroup counts processes (zombies included, as kill(-pgid, 0) does) whose
// process group id is pgid.
func countGroup(pgid int) int { return countGroupMembers(pgid, true) }

// countLiveGroup counts the members of the group that can still run: zombies are
// not counted. An unreadable /proc counts as one live member.
func countLiveGroup(pgid int) int {
	n := countGroupMembers(pgid, false)
	if n == 0 && !procReadable() {
		return 1
	}
	return n
}

func procReadable() bool {
	_, err := os.ReadDir("/proc")
	return err == nil
}

func countGroupMembers(pgid int, zombies bool) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		if statGroup(string(data)) == pgid && (zombies || !statZombie(string(data))) {
			n++
		}
	}
	return n
}

// statGroup extracts pgrp from /proc/<pid>/stat. The command name is
// parenthesized and may itself contain spaces and parentheses, so fields are
// counted after the last ')': state, ppid, pgrp.
func statGroup(stat string) int {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return -1
	}
	fields := strings.Fields(stat[i+1:])
	if len(fields) < 3 {
		return -1
	}
	pgrp, err := strconv.Atoi(fields[2])
	if err != nil {
		return -1
	}
	return pgrp
}

// statZombie reports whether /proc/<pid>/stat says the process is a zombie: the
// state is the first field after the last ')'.
func statZombie(stat string) bool {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return false
	}
	fields := strings.Fields(stat[i+1:])
	return len(fields) > 0 && fields[0] == "Z"
}
