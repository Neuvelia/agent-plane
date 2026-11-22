// Package proctree starts a child process and owns its whole process tree.
//
// Terminate follows one contract on every platform: request a graceful stop,
// wait a bounded grace, force, verify the tree is empty, and report how it
// ended. It signals only a tree this process created (a Unix process group the
// package made), never a process found by PID or name.
package proctree
