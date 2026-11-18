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
redacted retained event per line) plus `graph.yaml` (byte copy of the executed
definition). Lifecycle facts are `fsync`ed before the emitter returns; log and
progress chunks are synced with the next lifecycle fact. Directories 0700,
files 0600; the run directory is created exclusively. `agentplane run` stores
only with `--store-dir` (history is opt-in); `runs list/show` and the server
default to `.agentplane/runs`. Persistence is a synchronous, ordered sink on the
emitter (`Collector.SetPersist`, `runtime.Options.Persist`); it does not alter
events, and a sink failure is reported in `RunResult.PersistError` without
stopping the run. A graph containing a configured secret value is refused for
storage. A log without a terminal event is reported
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

Decisions are scoped to the run: a repeat for a node run that belongs to another
run is "not pending" (404) and never reveals the earlier decision. A decision the
hub accepted is delivered to the run even if cancellation races it. If a store
write fails during a served run, the run is stopped, nothing further is forwarded
to clients, streams end with an `error` event, and the run is listed with an
`error` explaining that it could not be stored. On shutdown the server cancels
active runs and waits up to five seconds for their terminal events.

`graph_path` is read by the sidecar with the local user's own permissions. The
desktop allowed a typed path as well as a picked one, so it was not bound to the
file dialog; the trust model is that the person at the keyboard chooses which
graph to execute, exactly as with `agentplane run`. The desktop renderer no
longer sends a path: it starts runs by grant (ADR-0021, decision 9), and the API
keeps accepting `graph_path` for other clients.

Durability scope: lifecycle events survive the process being killed. They are
not guaranteed across power loss: the run directory entry and `graph.yaml` are
not fsynced. Persistence runs under the collector lock, so a hung filesystem
blocks the run; there is no timeout.

Known limitations of this slice: `serve` has no `--redact-env` and no cap on
concurrent runs; finished runs stay in memory for the server's lifetime; a run
started by another process is replayed from the store but not followed.

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

## Amendment (proposed, 2026-11-17): additive `graph_summary` fields

**Status:** proposed amendment; becomes accepted when the F2.7 implementation PR is merged. Proposed by the lead
on the product owner's delegation (decision 3 of `docs/features/F2.7-run-inspector-node-detail.md`); the team may
revisit.

**Why.** The Run Inspector node detail (F2.7) must show which recorded value belongs to which node input. Today
`GET /runs/{id}` returns `graph_summary` with node IDs, types and node-level edges only, and the renderer must not
parse YAML (ADR-0021). Adding declared ports and port-level bindings server-side keeps one parser and needs no new
route, event or schema version.

**Change.** `graph_summary` on `GET /runs/{id}` gains, additively and derived from the run's stored `graph.yaml`:

- per node, `inputs` and `outputs`: arrays of `{name, type, required}`, sorted by name;
- top-level `bindings`: array of `{source, target}`, where `source` is `{node, port}` (a node output) or
  `{input}` (a graph input name) and `target` is `{node, input}`. Sorted by target node, target input, then
  source, so the order is deterministic.

Existing fields (`nodes[].id/type/max_attempts`, `edges`) and their order are unchanged. For a graph that does not
parse the new collections are empty, as `nodes` and `edges` are today. They carry names and types only, never
values, paths or secrets. A client that does not know the fields ignores them; a client that finds them absent
(older engine) reports bindings as unavailable.

**Compatibility.** No event schema, route, auth, CORS, bridge or store change; stored runs gain the fields because
the summary is computed on read. Validation is the F2.7 acceptance criterion on `summarizeGraph` (`go test
./internal/api/...`) plus the older-engine parse test in the renderer.
