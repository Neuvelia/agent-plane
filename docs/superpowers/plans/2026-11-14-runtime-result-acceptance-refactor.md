# Runtime Result Acceptance Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate result acceptance from `runtime.Run` without changing execution behavior.

**Architecture:** Keep the existing `runMachine` as the coordinator. Move only the ordered loop over completed nodes into a private `acceptBatchResults` method in `accept.go`. The method updates the existing `batchState`, records checks/envelopes/evidence, accepts outputs or records failure, and returns retry IDs. `Run` still owns batch iteration, failure selection and terminal run state.

**Tech Stack:** Go 1.23 and existing repository packages; no new dependency.

**Spec:** `docs/codebase-map.md`, `docs/features/F0.4-dag-scheduler.md`, `docs/features/F1.6-correction-loop.md`, `docs/features/F1.7-return-envelope-scoped-retry.md`, ADR-0004/0012/0014/0015/0017/0018.

## Global Constraints

- Preserve the public `Run`, graph, event, result and CLI contracts exactly.
- Keep coordinator ownership of state, outputs, event order and retry state.
- Preserve `GateCheck` → `ReturnEnvelope` → `AttemptFinished` order for enrolled units, and the existing standalone Gate path.
- Preserve the distinction between red retries, exhausted loops, operational failures, cancellation and rejected human decisions.
- Do not change worker dispatch (R3a), batch barriers or run finalization.

## Review Focus

- A red loop attempt must not publish outputs or rerun accepted siblings.
- A final red attempt must fail with `needs_intervention` and retain its check/envelope facts.
- Invalid Gate observation must fail closed and not emit a valid check fact.
- Concurrent cancellation and unrelated failure must retain the established precedence.
- Accepted unit values and provenance must be published once and remain detached.

---

### Task 1: Move ordered result acceptance

**Files:** Create `internal/runtime/accept.go`; modify `internal/runtime/scheduler.go`.

**Interfaces:** Private `(*runMachine).acceptBatchResults(ctx context.Context, definition *graph.Definition, results []completedNode, state *batchState) []string`. It processes already sorted results and returns retry node IDs.

- [ ] Confirm existing scheduler, Gate, loop, unit and human tests cover the review focus; add a public-`Run` test only for a real gap.
- [ ] Move the result loop without changing statement order, conditions, payloads or state transitions.
- [ ] Leave failed-node scan, retry cancellation, batch iteration and finalization in `Run`.
- [ ] Run `gofmt`, `go test ./internal/runtime/...`, `go test -race ./internal/runtime/...`, and `make check`.
- [ ] Review the full diff and commit one coherent refactor for independent review.
