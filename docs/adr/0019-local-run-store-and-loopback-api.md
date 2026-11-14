# ADR-0019 - Local JSONL run store and loopback API

**Status:** accepted
**Date:** 2026-11-13

## Context

Execution history must survive process exit, and a desktop UI must observe and
steer runs without re-implementing scheduling. AGENTS.md requires local-first,
no mandatory remote service, loopback-only listeners and the UI as an editor/
observer of the same contracts. The full durable store (F3.2) and SQLite index
(F2.9) are later; this slice needs the smallest durable seam.

## Decision

Persist each run as `<store>/<run_id>/events.jsonl` (one emitted, already
redacted event per line, `fsync` before the emitter returns) plus
`graph.yaml` (byte copy of the executed definition). Default store
`.agentplane/runs`; directories 0700, files 0600. Persistence is an emitter
sink; it does not alter events. A log without a terminal event is reported
`interrupted`; a corrupt tail is truncation, not a crash. No index, retention
or resume. Add `runs list/show` and `--store-dir`.

Expose a local HTTP API from `agentplane serve`: bind `127.0.0.1` ephemeral
port only; random 32-byte bearer token printed once on stdout as JSON for the
parent process; constant-time check; Host header must match; no CORS headers;
no remote bind option. Event delivery is replay-then-follow SSE read with
`fetch` (Authorization cannot be set on EventSource; the token never goes in a
URL). Decisions are idempotent (409 + original on repeat). Process exits when
stdin closes, cancelling active runs.

The Electron app is a client of this API. The UI derives state only by reducing
events; it does not schedule, retry or decide state.

## Alternatives considered

### Alternative A - SQLite now

Pros: queries, index.
Cons: new dependency and schema before the event contract needs it; JSONL
export is free and replay is the source of truth.

### Alternative B - Electron main embeds the engine (TypeScript)

Pros: single process.
Cons: duplicates the Go engine; violates "UI is not the scheduler".

### Alternative C - Unix socket / stdio only

Pros: no TCP surface.
Cons: awkward for streaming and tooling on all platforms; loopback + token is
sufficient and standard.

## Consequences

### Positive

- History survives restart with no new dependency; trivially exportable.
- One engine, two clients (CLI, desktop).

### Negative / trade-offs

- No query/index; listing scans directories.
- Local TCP port exists while serving (loopback, authenticated).
- No resume of interrupted runs; they are only readable.

## Compatibility and migration

Event schema unchanged by the store. A later SQLite/F3.2 store must be able to
ingest JSONL logs written here (they carry `event_schema_version` per event).

## Validation

F2.9 and F2.5 acceptance tests: roundtrip read in a fresh process, killed-run
`interrupted`, truncated tail, path traversal rejection, secret absence in
stored files, token/Host/401/421 behavior, mid-run stream no-gap/no-duplicate
test, `make check` and race run.
