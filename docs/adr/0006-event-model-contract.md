# ADR-0006 - In-memory event and execution identity contract

**Status:** accepted
**Date:** 2026-10-18

## Context

F0.4 owns deterministic scheduler transitions and an in-memory transition trace. F0.5 captures Shell output, but neither layer exposes a structured event contract or execution correlation IDs. F0.6 needs an ordered, serializable record without introducing persistence or a live delivery system.

## Decision

- A started run receives a random UUID v4 run ID. Each defined node receives one UUID v4 node run ID for that run, including a node that never starts. Attempt ID is reserved and omitted until an attempt lifecycle exists. `(run_id, sequence)` identifies an event; there is no separate event ID.
- Event JSON has `event_schema_version: 1`, stable snake-case field names, UTC timestamp, per-run sequence starting at 1, graph ID, graph version, optional node identity, and a payload selected by event type. `sha256-v0:` prefixes the graph version fingerprint.
- The fingerprint hashes a normalized schema-v0 definition without `name` or `metadata`. Edge and graph-input-binding order is sorted because it does not alter execution semantics. Node configuration and argv order remain significant.
- The run-local `events.Collector` assigns sequence and timestamp under one mutex and keeps a detached in-memory copy. `RunResult.Events` is the complete consumer surface for F0.6. The scheduler does not invoke arbitrary external observers while executing.
- The collector closes after `run_finished`; later append attempts return an error. Invalid event type/payload pairs are rejected before storage and JSON encoding. JSON decoding rejects fields from an unrelated payload type. Optional executors must finish log callbacks before returning to the scheduler for complete logs. The scheduler closes each observer after its final flush and discards callbacks that arrive later, so a faulty executor cannot append after the run finishes.
- The coordinator emits lifecycle events from the same methods that mutate state. Workers may emit Shell log chunks through the collector. Each stream remains distinct; chunks are bounded to 16 KiB and JSON represents raw bytes as base64.
- `node_finished` means a node that entered running reached a terminal state. A pending or ready node that fails, skips, or cancels emits `node_terminated`. This preserves the invariant that every `node_finished` follows `node_started` while representing F0.4's actual state transitions.
- Event payloads omit graph inputs, node outputs, full process environments, command configuration, and raw error text. Failure kind and diagnostic codes are the event-safe summary. `runtime.Options.LogRedactions` supplies exact strings for deterministic, per-stream redaction before chunks enter the collector. The replacement marker repeats a byte absent from every secret, preventing the marker itself or its boundaries from revealing one. If no such byte exists, setup fails before a run starts. There is no arbitrary caller callback on the process pipe-copy goroutines.

## Alternatives considered

### ULIDs

ULIDs can be sorted by generation time, but event sequence already determines order. UUID v4 avoids clock-dependent identity and needs only the standard library.

### Synchronous or asynchronous external callbacks

A synchronous callback can block execution. An asynchronous subscriber needs buffering, delivery, and shutdown rules. Returning the complete in-memory event slice gives F0.7 a machine-readable source without deciding live-delivery semantics prematurely.

### Use `node_finished` for unstarted nodes

This would imply that a node finished execution after never starting. A distinct `node_terminated` event preserves the scheduler's state facts and the start-before-finish invariant.

## Consequences

- Run identity and event sequence are independent of Go map iteration. Concurrent logs have an authoritative arrival order, but their cross-stream order may differ between executions.
- `StateTransition.Sequence` is the matching lifecycle event sequence. Log events can make the transition-only sequence appear to have gaps.
- A run with `run_started` and no `run_finished` is recognizable as incomplete if these events are persisted later; F0.6 does not persist or recover it.
- Shell capture and event chunks both remain in memory. Aggregate log retention is unbounded in this phase.
- The default event builders do not intentionally serialize configured secrets. A child may print a secret; callers must list values that need redaction. The redactor catches listed values across write boundaries independently in stdout and stderr. Unlisted output remains visible in event logs, and local Shell process evidence remains raw.

## Compatibility and migration

Graph schema v0 and scheduler state semantics do not change. Run and node results gain IDs, graph version, and events. Previously deterministic comparisons of entire results must ignore per-run IDs and timestamps. A later event format change must increment `event_schema_version`; a fingerprint algorithm change must use a new prefix.

## Validation

Tests cover graph fingerprint stability and changes, UUID uniqueness, payload JSON round trips and mismatch rejection, concurrent event sequencing, lifecycle ordering, unstarted-node terminal events, cancellation, Shell streams, complete large output, boundary-spanning redaction, and marker safety. Required gates are `go test -race ./...` and `make check`.
