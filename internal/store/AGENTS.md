# Run store guidance

The root `AGENTS.md` applies here. This file adds rules for
`internal/store/` source and tests. The store owns local run-history files;
it does not execute graphs or define event payloads.

## Find the owner

| Concern | Start here |
| --- | --- |
| JSONL writer, directory layout, permissions and fsync | `store.go`: `Writer`, `Append`, `open`, `Close` |
| Readback, summaries, corruption and run-ID validation | `store.go`: `Read`, `List`, `ValidRunID` |
| Store preflight and CLI `run`/`runs list/show` behavior | `cmd/agentplane/cli.go`, `cmd/agentplane/runs.go` |
| Served-run persistence and client-visible failure handling | `cmd/agentplane/serve.go`, `internal/api/server.go` |
| Event schema, retention and sequence assignment | `internal/events/AGENTS.md`, `internal/events/emitter.go` |

## Preserve these contracts

- A run has `<store>/<run_id>/events.jsonl` and a `graph.yaml` byte copy of
  the executed definition. `NewWriter` is inert until the first `Append`;
  the first event supplies the run ID. Accept only lowercase UUID-shaped run
  IDs for directory names and reads. Use an exclusive run directory and log
  creation so a second writer cannot overwrite a run. Newly created
  directories use 0700 and files 0600; `MkdirAll` does not tighten an
  existing store directory. Do not widen permissions or trust request paths.
- Write one already-redacted, retained event per JSON line without changing
  its payload or sequence. The collector decides retention and assigns
  sequence; gaps from omitted progress are valid. A non-progress event is
  synced before `Append` returns. Progress/log chunks are written and synced
  by the next non-progress event or `Close`. This protects acknowledged
  lifecycle facts against process kill, not power loss: directory entries
  and `graph.yaml` are not fsynced. A blocked filesystem can block the run.
- `Read` must validate the event schema, run ID, increasing sequence, first
  `run_started` and absence of facts after `run_finished`. A log without a
  terminal fact is `interrupted`; an invalid or incomplete final line may be
  marked `truncated` while earlier events remain readable. Mid-log
  corruption is an error. `List` scans directories newest first, skips
  foreign entries, and reports unreadable run directories as `corrupt` with
  a reason instead of silently dropping them. There is no index, retention
  policy or recovery of interrupted runs in this slice.
- CLI `run` stores history only with `--store-dir`; `runs list/show` and
  `serve` default to `.agentplane/runs`, and the server stores every run.
  `Check` probes writability before execution. The writer latches the first
  write/sync failure and stops further writes; `Close` flushes and closes an
  opened log. The caller owns error policy: CLI `run` still presents the
  execution result and exits with a persistence error; `serve` cancels a run
  and stops forwarding events when storage fails. Do not move scheduler or
  client policy into `store`.
- The store copies graph bytes and adds no redaction. CLI `run` refuses to
  store a graph containing a configured `--redact-env` secret, including a
  parsed string value hidden by YAML escapes or folding. Keep this check
  before any run starts. Event redaction happens before `Writer.Append`;
  neither the store nor its file permissions make arbitrary graph bytes safe
  to persist.

The active contract is `docs/features/F2.9-run-store.md` and
`docs/adr/0019-local-run-store-and-loopback-api.md`. Read
`docs/features/F0.6-event-model.md` and
`docs/adr/0006-event-model-contract.md` when changing replay or event
identity. The store was introduced as a minimal JSONL slice; later SQLite,
indexing, retention and resume are separate roadmap work. A change to the
durable file format or fsync guarantee needs a feature spec and ADR under
the root policy.

## Choose regression tests

- File layout, permissions, IDs, exclusive creation, sync order and fault
  latching: `store_test.go`.
- Round trip, incomplete/corrupt logs, ordering and list behavior:
  `store_test.go`.
- Opt-in history, CLI replay, killed runs, secret refusal and persistence
  errors: `cmd/agentplane/store_test.go`.
- Served-run persistence, corrupt lists and storage failure delivery:
  `cmd/agentplane/serve_test.go`, `internal/api/server_test.go`.

Run `go test ./internal/store/...` before `make check`. For changes to the
collector-to-store handoff, also run focused `internal/events`, runtime, CLI
and API tests. Update this guide when a named owner or contract changes.
