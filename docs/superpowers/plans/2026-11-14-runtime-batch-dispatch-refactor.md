# Runtime Batch Dispatch Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate worker dispatch and result collection from `runtime.Run` without changing execution behavior.

**Architecture:** Keep the coordinator and all state maps in `internal/runtime`. A private batch state groups the existing per-node attempt IDs, numbers, feedback and Git baselines. A private `runBatchWorkers` method starts the same workers in the same order, waits for every started worker, and sorts completions by node ID. Result acceptance and retry remain in `Run` for R3b.

**Tech Stack:** Go 1.23, standard library and existing packages.

**Spec:** `docs/codebase-map.md`, `docs/features/F0.4-dag-scheduler.md`, `docs/features/F1.6-correction-loop.md`, ADR-0004, ADR-0014, ADR-0015 and ADR-0018.

## Global Constraints

- Public `Run`, graph, event and result contracts remain unchanged.
- The coordinator alone mutates node states, attempt maps and accepted outputs.
- Worker goroutines return values through the existing buffered channel; completion is sorted before acceptance.
- Preserve cancellation checks, attempt lifecycle facts, unit begin and retry bookkeeping order.
- No new dependency, package or behavior. R3b owns result acceptance decomposition.

## Review Focus

- Cancellation during dispatch must not launch later nodes.
- Attempt ID creation or unit begin failure must still fail the node without launching its worker.
- A started worker must be joined even if cancellation occurs during the batch.
- Completion order must remain sorted by node ID, regardless of goroutine finish order.
- Retry state must survive red attempts while accepted siblings remain untouched.

---

### Task 1: Extract batch worker dispatch

**Files:** Create `internal/runtime/batch.go`; modify `internal/runtime/scheduler.go`.

**Interfaces:** Private `batchState` stores the four existing per-node maps. `runBatchWorkers(ctx context.Context, executor Executor, batch []string, state *batchState, redactor *logRedactor) []completedNode` runs and joins workers.

- [ ] Confirm existing scheduler, loop and unit tests cover the review focus; add a public-`Run` regression test only for a real gap.
- [ ] Move only attempt setup, worker launch, collection and sorting into `batch.go`; keep statement order and callback capture unchanged.
- [ ] Leave result acceptance, retries and run termination in `Run`.
- [ ] Run `gofmt`, `go test ./internal/runtime/...`, `go test -race ./internal/runtime/...`, and `make check`.
- [ ] Inspect the complete diff and commit one coherent change for independent review.
