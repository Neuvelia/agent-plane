# Change Inventory Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a read-only, accurate local change inventory for future
selective verification.

**Architecture:** A small Go command under `scripts/verifychanges/` calls Git
with fixed argv and NUL-delimited output, unions change sources and writes
deterministic JSON. `Makefile` exposes it; no product package imports it.

**Tech Stack:** Go standard library, Git, Make.

**Spec:** `docs/superpowers/specs/2026-11-16-change-inventory-design.md`.

## Global Constraints

- The inventory command must not run checks, print file contents or mutate
  Git/worktree state.
- Preserve both sides of renames and all three source classes.
- A failed Git query must never emit success-shaped JSON.
- No product API, event, graph, CLI or dependency changes.

## Review Focus

- Staged/unstaged union, especially a worktree revert of a staged edit.
- NUL delimiters, unusual filenames and deterministic JSON order.
- Subdirectory invocation, ignored files and Git failure behavior.
- Clear distinction between inventory success and verification success.

---

### Task 1: Expose and test the change inventory

**Files:** Create `scripts/verifychanges/main.go` and
`scripts/verifychanges/main_test.go`; edit `Makefile`, `scripts/check.sh` and
`docs/development-workflow.md`; create this design/plan pair.

**Interface:** `make verify-changes` prints JSON containing
`checks_executed: false` and sorted path/source records.

- [x] Write temp-Git-repository tests for source union, staged revert,
  rename, subdirectory/ignored paths, unusual filenames and non-repo error.
- [x] Run the focused tests and observe the missing-behavior failure.
- [x] Implement only the fixed Git queries, path union and JSON output.
- [x] Run focused tests, then `make check`.
- [x] Inspect the full diff and obtain independent read-only review.
- [ ] Commit, push, create PR and verify its hosted checks.
