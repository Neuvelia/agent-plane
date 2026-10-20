//go:build !darwin && !linux

package desktop

// Task 10 must verify native replacement and directory durability on Windows.
const registryReplaceAvailable = false
