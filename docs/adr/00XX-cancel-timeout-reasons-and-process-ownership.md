# ADR-00XX - Cancel and Timeout Reasons, Cross-Platform Process Ownership

**Status:** proposed (number assigned at merge)
**Date:** 2026-11-17

Product name: Tarmind (in code today: agentplane).

## Context

F3.4 adds time limits and a uniform cancel. Two durable contracts change:

1. Event and state semantics. `cancelled` already exists as a terminal node and
   run state but carries no cause. Timeouts need a trace-visible, typed reason
   that is not confused with failure.
2. The process model. Codex owns a Unix process group, Shell kills only its
   direct child, and Windows owns nothing. Windows is supported in the first
   release, so a tree-ownership model is needed there.

## Decision

1. **No new terminal status.** `cancelled` and `failed` stay the only
   non-success terminal states. A typed, closed `reason` is added to
   `node_finished`, `node_terminated` and `run_finished`: `interrupt`,
   `user_cancel`, `run_timeout`, `sibling_failed`, `node_timeout`. A run deadline
   ends the run `cancelled`/`run_timeout`. A node deadline ends the attempt
   `failed` with `failure_kind: timeout` and consumes one loop attempt.
2. **New events** `cancel_requested` and `process_terminated` (schema 12,
   additive). The reason is derived from the `context.Cause` of a typed cause,
   never from error text or exit code.
3. **One termination contract** for every owned child: own the tree, request,
   bounded grace, force, verify empty, idempotent, classify by our own record.
   Unix: process group, TERM then KILL. Windows: Job Object with
   `KILL_ON_JOB_CLOSE`, no breakaway, `CTRL_BREAK_EVENT` best effort, then
   `TerminateJobObject`. `golang.org/x/sys/windows` becomes a direct
   dependency (Windows build only).
4. Limits are opt-in graph fields (`timeout_seconds`, `limits.run_timeout_seconds`)
   in the semantic graph version.

## Alternatives considered

### A dedicated `timed_out` terminal status

Pros:

- Explicit in every consumer.

Cons:

- Changes the state machine, every exhaustive switch (CLI, API, desktop, store)
  and node counts; a run deadline is conceptually a cancel and a node deadline is
  conceptually a failed attempt that the loop already knows how to bound.

### Reason only in `failure_kind`

Pros:

- No payload change.

Cons:

- A cancelled node has no `failure_kind`; interrupt versus run deadline would
  stay indistinguishable.

### Windows: kill by PID tree walk (`taskkill /T`, toolhelp snapshot)

Pros:

- No Job Object code.

Cons:

- Racy against spawn and PID reuse, cannot cover descendants whose parent died,
  no crash-orphan safety.

### Windows: no ownership, kill the leader only

Pros:

- Trivial.

Cons:

- Descendants survive; violates the F1.2 and F3.4 guarantees.

## Consequences

### Positive

- Trace answers why a unit stopped; CLI and UI can distinguish interrupt,
  deadline and failure.
- Same observable guarantees on Linux, macOS and Windows; Windows crash orphans
  are reaped by the OS.
- Exit code and OS wait-status quirks no longer decide the outcome.

### Negative / trade-offs

- Event schema 12 and a new payload field; readers must ignore unknown fields.
- Windows graceful stop is best effort; only forced termination is guaranteed.
- A Job Object is lifetime control, not confinement; escaped Unix `setsid`
  descendants remain out of scope.

## Compatibility and migration

Graph fields are additive and optional; no graph schema version bump. Schema 12
readers accept 0-11; older readers ignore `reason`. Existing graphs and logs
behave unchanged. The CLI keeps exit 130 for interrupt and user cancel and adds
one code for a run deadline.

## Validation

The F3.4 and F3.4a acceptance criteria: reason tests with a fake clock,
process-tree tests on all three OSes, Windows W1-W7, and removal of
`experimental` from `check (windows-x64)` only after the F3.4a conditions hold.
