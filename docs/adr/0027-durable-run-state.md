# ADR-0027 - Durable run state on an append-only JSONL log

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-17
**Extends:** ADR-0019 (local JSONL run store)

## Context

Tarmind (in code today: agentplane) stores each run as `events.jsonl` plus
`graph.yaml` (ADR-0019, F2.9). Lifecycle events are fsynced, `interrupted` is a
read-time guess, listing parses every log, nothing is retained or bounded, and
creation is not durable. F3.2 (`docs/features/F3.2-durable-run-state.md`) must
make the store a durable source of truth that F3.3 resume can build on.
Constraints: AGENTS.md local-first, dependency policy (standard library first),
trace is correctness, first release supports Windows, single binary.

## Decision

Keep JSONL and extend it. The event log is the only authoritative state;
everything else is a rebuildable optimisation. No database.

1. **Transition boundary.** A transition is complete when its event line is
   written and fsynced; the sink completes before delivery to subscribers or
   scheduler progress. Progress/log chunks keep deferred sync.
   *Rejected:* fsync every line (cost on chatty agents, no correctness gain);
   batching lifecycle events (loses acknowledged transitions on kill).
2. **Interrupted.** Derived state: no terminal event and no live writer, where
   liveness is an OS advisory lock on `run.lock` held for the run's life and
   released by the kernel on process death. Exposed as store status
   `interrupted`, an API `interrupted_at_sequence`, and derived node/attempt
   `interrupted`. No new event or schema bump in F3.2.
   *Rejected:* a recorded `run_interrupted` event now (needs a writer on a dead
   run's log, a schema bump and recovery semantics that belong to F3.3);
   PID file or heartbeat (stale PIDs, clock dependence, PID reuse).
3. **Older data.** Read-forward only: schemas 1-11 stay readable via the
   existing decoder; a newer schema is `unsupported` and never modified; a
   `store_format` marker (absent = 0) guards the layout. No rewrite migration.
   *Rejected:* rewriting old logs (breaks the audit-source property and
   Git-diffable exports).
4. **Index.** Per-run `summary.json` snapshot, validated against the log
   size and terminal state, with the log winning on any disagreement. A
   store-wide `index.json` is added only if the benchmark fails.
   *Rejected:* store-wide index first (single hot file to corrupt and lock);
   pure directory scan (O(runs x events)).
5. **Retention.** Off by default; local limits in `store.json`; applied only
   by explicit `runs prune` (and at `serve` start if configured); never touches
   live or corrupt runs. *Rejected:* automatic background deletion (surprises
   users, destroys evidence silently).
6. **Windows.** Use `FlushFileBuffers` (`File.Sync`) for the log; treat
   directory fsync as unavailable; rely on no rename for authoritative state
   (the snapshot is atomic-ish and disposable); `LockFileEx` for liveness;
   delete failures from sharing violations are skipped and reported.

### Rejected alternative: SQLite

Pros: indexed queries, transactional metadata, WAL durability. Cons: violates
the dependency policy unless justified; the mature driver needs cgo, which breaks
the single static cross-compiled binary and complicates Windows/macOS
distribution; a pure-Go port (e.g. a transpiled driver) adds a large, fast-moving
dependency and its own file-format and crash-semantics risks; the log would
still be needed as the audit source, creating two sources of truth; JSONL is
trivially exportable, diffable and recoverable by hand. The access pattern
(append, list runs, replay one run) does not need SQL. Revisit only if a measured
query need (cross-run search) cannot be met by snapshots; that would be a new
ADR, with the JSONL log kept as the source of truth.

## Consequences

Positive: no new heavy dependency (`golang.org/x/sys` is already required),
exact crash semantics, fast listing, bounded disk use on request, one audit
source. Negative: cross-run queries stay linear in snapshots; directory
fsync is best-effort on Windows; advisory locks do not protect against a
non-cooperating process or some network filesystems (documented as unsupported
store locations); more files per run.

## Compatibility and migration

Event schema stays 11. Stores written by ADR-0019 are read unchanged; new files
appear lazily on first write. A binary older than this change ignores
`summary.json`, `run.lock` and `store.json` (it only scans run directories
for `events.jsonl`); it still lists runs correctly, minus liveness.

## Validation

`kill -9` crash test on Linux and Windows; fault-injected sync/write failures;
schema fixtures 1-11 plus a schema-12 fixture marked unsupported; snapshot-staleness and listing benchmark at 5,000 runs;
prune behaviour; `make check` and `go test -race ./internal/store/...`.
