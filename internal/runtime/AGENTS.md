# Runtime package guidance

The root `AGENTS.md` still applies. This file adds rules for
`internal/runtime/` source and tests.

## Find the owner

| Concern | Start here |
| --- | --- |
| Run setup and graph/input preflight | `preflight.go`, then `scheduler.go` |
| Readiness, batch barriers, terminal states | `scheduler.go`, `state.go`, `result.go` |
| Worker dispatch and ordered result acceptance | `batch.go`, `accept.go` |
| Bounded attempts, Gate observations, accepted units | `loop.go`, `gate.go`, `unit.go` |
| Human approval and cancellation | `human.go` |
| Workspace identity and changed-file baselines | `workspace.go`, `loop.go` |
| Bounded progress, logs and redaction | `progress.go`, `log_redactor.go` |
| Opt-in recorded run values (ADR-0023) | `values.go`, called from `preflight.go` and `accept.go` |
| Opt-in worktree isolation (ADR-0024): port, preflight, lifecycle, integration, release | `isolation.go`, called from `preflight.go`, `batch.go`, `accept.go`, `scheduler.go` (`finish`) |

## Preserve these contracts

- Keep validation and capability checks in preflight before `newRunMachine`
  emits `run_started`. A normal preflight failure has no run identity or
  execution events.
- The coordinator owns node/run state, retry state and accepted outputs.
  Workers return `completedNode` observations. Results are accepted in sorted
  node-ID order after the current batch finishes; a later batch cannot start
  early. Preserve lowest-ID failure selection and the established precedence
  of unrelated failures over concurrent cancellation.
- For loop producers, only a valid green check publishes candidate outputs.
  A valid red check retries only that node within its budget; exhausted loops
  need intervention. Enrolled units accept once and return detached values.
  Git-observed scope checks detect changes after execution; neither acceptance
  nor scope checking freezes or isolates files in a shared workspace.
- For a valid enrolled attempt, preserve the relative order `gate_check` ->
  `return_envelope` -> `attempt_finished`. Operational errors must not invent
  an envelope. Lifecycle facts accompany state transitions; bounded worker
  logs/progress and human approval facts also use the event collector. Keep
  secrets and raw error text out of events.
- `Options.RecordValues` is off by default; when off, no `value_recorded`
  fact exists. When on, supplied graph inputs are recorded directly after
  `run_started` and each accepted output port directly before the node's
  succeeded transition, never for failed, skipped, cancelled or rejected
  work. Redact the whole text with the configured redactor before cutting,
  hash the redacted text, cut at a rune boundary and charge the per-run
  budget on the coordinator only.
- `isolation: worktree` (ADR-0024) is opt-in and valid only on an enrolled loop
  Agent; everything is validated in preflight before `run_started`, including a
  single source repository per run. The coordinator alone creates worktrees
  (before the first attempt, serially), snapshots and integrates accepted units
  in sorted node order after the unit's values are frozen and before the
  succeeded transition, and releases worktrees of terminal nodes after every
  batch's acceptance (`releaseIsolated` in `Run`), with a final sweep in
  `finish` before `run_finished` for the rest (plan: terminal states plus a
  sweep). Workers receive a deep-copied node whose Agent and Gate
  workspace is the worktree; the graph and semantic version are never changed.
  A conflict fails the node as `needs_intervention` (outputs stay recorded,
  nothing is rerun); snapshot, integrate and create failures are `executor`
  failures with fixed diagnostic codes and no raw Git text. Release uses a
  context detached from cancellation but bounded by `Options.CleanupTimeout`
  (default 30s; a timeout reports `cleanup_failed` / `remove_timeout`) and never
  changes run status. Isolated
  nodes get a synthetic, never-conflicting scheduling identity. Production
  `runtime` reaches Git only through `WorkspaceManager`; it must not import
  `internal/worktree`.
- Production `runtime` imports `graph`, `events` and `gitstate` and defines
  small consumer-owned executor interfaces. Put concrete Agent, Gate, Shell,
  API and store behavior in their owning packages; do not import them into
  production `runtime`. External `runtime_test` integration tests may compose
  concrete packages such as `agent` and `gate`.

Before changing a contract, read its feature spec and ADR:

- Scheduling/events: `docs/features/F0.4-dag-scheduler.md`,
  `docs/adr/0004-in-memory-scheduler-contract.md`,
  `docs/adr/0006-event-model-contract.md`.
- Correction/units: `docs/features/F1.6-correction-loop.md`,
  `docs/features/F1.6a-shell-loop.md`,
  `docs/features/F1.7-return-envelope-scoped-retry.md`,
  `docs/adr/0014-bounded-node-correction-loop.md`,
  `docs/adr/0015-return-envelope-and-unit-acceptance.md`,
  `docs/adr/0018-shell-node-correction-loop.md`.
- Scope/human: `docs/features/F1.8-changed-files-scope-check.md`,
  `docs/features/F2.10-human-node.md`,
  `docs/adr/0016-changed-files-scope-check.md`,
  `docs/adr/0017-human-node-and-local-approval.md`.

## Choose regression tests

- Scheduling, failure and cancellation: `scheduler_test.go`, `state_test.go`,
  `events_test.go`.
- Loops, Gates and units: `loop_test.go`, `gate_test.go`,
  `unit_integration_test.go`, `unit_registry_test.go`.
- Human decisions, workspace scope and observations: `human_test.go`,
  `workspace_test.go`, `changed_scope_integration_test.go`,
  `observation_test.go`, `progress_test.go`.
- Recorded values: `values_test.go`, `values_loop_test.go`.
- Worktree isolation: `isolation_test.go` (fake `WorkspaceManager` in
  `isolation_fake_test.go`) for preflight, scheduling, lifecycle, events,
  retention and result; `isolation_git_test.go` composes `internal/worktree`
  over temporary repositories for source-untouched, scoped retry, determinism,
  conflict and scope-gate behavior.

Run `go test ./internal/runtime/...` before the repository gate. For changes
to concurrency, cancellation or observer callbacks, also run
`go test -race ./internal/runtime/...`. Keep tests based on explicit barriers
or channels rather than timing sleeps. Update this guide in the same PR when a
named owner or contract changes.
