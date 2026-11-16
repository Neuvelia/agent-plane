# Event package guidance

The root `AGENTS.md` applies here. This file adds rules for
`internal/events/` source and tests. Event JSON is a durable contract used by
the CLI, local run store, loopback API and desktop client.

## Find the owner

| Concern | Start here |
| --- | --- |
| Event envelope, type dispatch, schema compatibility and JSON decoding | `event.go` |
| Run-local sequence, retention, optional persistence and live delivery | `emitter.go` |
| Bounded normalized progress and omission payloads | `progress.go` |
| Check Results, typed evidence and legacy schema-4 Gate facts | `gate.go`, `gate_legacy.go` |
| Attempt lifecycle and trace lookup | `attempt.go` |
| Return Envelopes, accepted-unit scope and declared conflicts | `unit.go` |
| Human approval facts and bounded text | `approval.go` |
| Opt-in recorded run values (schema 10) and their bounds | `value.go` |
| Workspace isolation facts, closed reason vocabulary, path bounding | `workspace.go` |

## Preserve these contracts

- `Event` is an append-oriented fact identified by `(run_id, sequence)`.
  `Collector` assigns run context, UTC timestamp and a monotonically
  increasing sequence under one lock. Preserve node/attempt correlation and
  the distinction between `node_finished` after a start and
  `node_terminated` before one. The runtime owns state transitions and
  decides when to emit facts; this package owns their shape and collection.
- New events use `SchemaVersion` 10. JSON readers accept schemas 1 through 10
  with their actual historical payload shapes; in-memory zero is a builder
  convention, not a serialized schema. Keep strict type/payload matching,
  unknown payload-field rejection and the schema-4 legacy Gate payload distinct
  from full Check Results introduced in schema 5. Do not synthesize missing
  historical scope, duration or attempt metadata. `value_recorded` (schema
  10, ADR-0023) carries at most `MaxValueBytes` of already-redacted UTF-8
  text; `value` is omitted only with `truncated: true`, and an untruncated
  value must match its `size_bytes` and `sha256`. The runtime owns redaction,
  the per-run `MaxRunValueBytes` budget and whether recording is enabled.
- Lifecycle, evidence, checks, attempt facts, Return Envelopes, approval
  and workspace isolation facts (schema 11, ADR-0024) survive progress retention limits. Logs and normalized progress share
  bounded retained and live budgets. An omitted progress event still consumes
  a sequence, so retained traces may have gaps. `progress_omitted` records
  retained and live omissions before `run_finished`; final facts are replay
  only. Live delivery is a bounded FIFO prefix and a slow reader must not
  block `Append`.
- `SetPersist` is an optional synchronous recorder for retained events. It
  runs in sequence order under the collector lock, including terminal facts;
  after its first error, `PersistError` reports the cause and recording stops.
  Keep returned events and snapshots detached from retained payload slices
  and pointers. The store owns durable JSONL history; do not put storage or
  runtime state decisions into this package.
- Keep exposed content bounded at each defined payload boundary; total
  lifecycle and evidence retention has no global fixed limit. Do not add raw
  secrets, private model reasoning, full process environments or unchecked
  error text. Runtime redacts configured values before they reach the
  collector; the collector does not discover or redact secrets. A new payload
  needs explicit validation, cloning, JSON dispatch, schema compatibility
  and tests for malformed and historical data.

Read `docs/features/F0.6-event-model.md` and
`docs/adr/0006-event-model-contract.md` for the base event contract. For
progress, checks, attempts, units and approvals, read the matching active
feature spec and ADR: `F1.3`/ADR-0011, `F1.5`/ADR-0013,
`F1.6`/ADR-0014, `F1.7`/ADR-0015, `F1.8`/ADR-0016 and
`F2.10`/ADR-0017 and `F3.1`/ADR-0024 under `docs/features/` and `docs/adr/`. A change to a
durable event shape or semantics needs its own feature spec and ADR under
the root policy.

## Choose regression tests

- Envelope, type/payload pairs and historical decoding: `event_test.go`,
  `progress_test.go`, `check_result_test.go`.
- Concurrency, sequence, retention, live prefix, persistence and copy
  isolation: `emitter_test.go`, `progress_test.go`.
- Checks, attempts, units, changed-file scope and approvals: `gate_test.go`,
  `attempt_test.go`, `unit_test.go`, `changed_files_test.go`,
  `approval_test.go`, `value_test.go`; workspace facts, bounds and retention:
  `workspace_test.go`.
- Producer ordering and redaction: `internal/runtime/events_test.go`,
  `internal/runtime/progress_test.go`; durable replay and API delivery:
  `internal/store/` and `internal/api/` tests.

Run `go test ./internal/events/...` before `make check`. For collector
concurrency changes, also run `go test -race ./internal/events/...`.
Update this guide when an event owner or contract changes.
