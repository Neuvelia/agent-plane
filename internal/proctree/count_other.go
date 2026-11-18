//go:build unix && !linux

package proctree

// countGroup is a lower bound: without /proc the caller only knows the group is
// not empty.
func countGroup(int) int { return 1 }
