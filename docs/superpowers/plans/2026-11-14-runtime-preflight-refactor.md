# Runtime Preflight Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `runtime.Run` easier to inspect by moving its preparation logic into focused internal functions without changing behavior.

**Architecture:** Keep the existing `runtime` package and public `Run` signature. An internal preparation value carries validated bindings, copied inputs, redaction configuration, unit scopes and concurrency limit. A second helper creates the run machine, registers units, inspects workspaces and calculates dependencies. The scheduler loop stays in `Run` for the later R3 PR.

**Tech Stack:** Go 1.23, standard library and existing repository packages.

**Spec:** `docs/codebase-map.md` (refactor invariants), `docs/features/F0.4-dag-scheduler.md` (scheduler contract), and ADR-0004/0014/0015/0017/0018/0019.

## Global Constraints

- No change to graph, event, CLI, store or API contracts; no new dependency or package.
- Preserve the exact order of preflight checks and their error values.
- No run identity, event or persistence call on ordinary preflight failure.
- Keep coordinator ownership of state, output and event order.
- Keep the scheduling loop, batch barrier and retry logic out of this PR.

## Review Focus

- Invalid options must fail before graph validation or adapter detection.
- Invalid graph/input/unit scope must fail before loop capability checks.
- Missing human provider must fail before run creation.
- Input values must be snapshotted before any worker can use them.
- Unit registration and workspace inspection must remain after machine creation and retain current event order.

---

### Task 1: Characterize preflight boundaries

**Files:** `internal/runtime/scheduler_test.go`, `internal/runtime/events_test.go` (only if a real contract gap is found).

- [ ] Confirm existing tests cover graph/input diagnostics, unit scope, loop and human preflight, redaction, and no run identity on ordinary preflight failure.
- [ ] Add a focused public `Run` test only for a missing behavior contract, such as rejected options producing no events or persistence calls; avoid tests of the new helper itself.
- [ ] Run `go test ./internal/runtime/...` before production edits and record the baseline.

### Task 2: Extract run preparation

**Files:** Create `internal/runtime/preflight.go`; modify `internal/runtime/scheduler.go`.

**Interfaces:** Internal only. `preflightRun(ctx, definition, inputs, executor, options) (runPreparation, error)` returns validated inputs, bindings, scopes, redactor and limit. `initializeRun(ctx, definition, options, preparation) (*runMachine, map[string][]string, error)` creates the machine and dependency map.

- [ ] Move the existing checks into `preflightRun` without reordering or changing their conditions or errors.
- [ ] Move machine setup, unit registration, workspace inspection and dependency collection into `initializeRun` without changing their order.
- [ ] Leave the scheduler loop as-is apart from reading values from `runPreparation`.
- [ ] Run `gofmt`, `go test ./internal/runtime/...`, `go test -race ./internal/runtime/...`, and `make check`.
- [ ] Review `git diff --stat`, complete changed files, event/state contracts and temporary files; commit one coherent refactor.
