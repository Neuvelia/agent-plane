//go:build unix && !linux

package proctree

// countGroup is a lower bound: without /proc the caller only knows the group is
// not empty.
func countGroup(int) int { return 1 }

// countLiveGroup is a lower bound too: without /proc a zombie cannot be told
// apart from a live process, so the group is never reported empty by it.
func countLiveGroup(int) int { return 1 }
