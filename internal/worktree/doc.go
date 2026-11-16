// Package worktree creates, snapshots, integrates and removes the linked Git
// worktrees that F3.1 uses to isolate Agent units (ADR-0024).
//
// It performs Git operations only. It does not schedule, decide acceptance or
// emit events; the runtime owns those and reaches this package through a small
// consumer-owned interface. A worktree is a separate directory, not a sandbox.
package worktree
