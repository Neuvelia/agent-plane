//go:build linux

package proctree

import (
	"os"
	"strconv"
	"strings"
)

// countGroup counts processes (zombies included, as kill(-pgid, 0) does) whose
// process group id is pgid.
func countGroup(pgid int) int {
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
		if statGroup(string(data)) == pgid {
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
