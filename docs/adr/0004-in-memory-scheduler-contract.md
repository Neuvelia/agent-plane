# ADR-0004 - In-memory DAG scheduler contract

**Status:** accepted
**Date:** 2026-10-07

## Context

F0.3 supplies immutable dependency and binding plans, but no execution state.
F0.4 must allow independent work to overlap while keeping results, failure
selection, and state transitions stable across goroutine completion orders.
The later Shell Node, event model, and CLI need a narrow execution boundary.

## Decision

- `runtime.Run` performs semantic and graph-input preflight before starting a
  run. Invalid input returns structured diagnostics and no run result.
- A single coordinator owns node state, run state, outputs, and transition
  sequence. Executor goroutines only return completion values through a
  buffered channel.
- Eligible nodes are ordered by node ID and dispatched in batches up to one
  global concurrency limit. A batch is committed in node-ID order before the
  next batch starts. Zero concurrency defaults to one; negative values fail
  preflight.
- Readiness requires every direct producer to succeed and F0.3 input
  resolution to pass. Graph outputs are materialized from compiled references
  after successful node execution.
- Any unhandled node failure stops future batches. Already running batch peers
  may finish; unstarted work becomes skipped. The lowest failed node ID is the
  primary failure. An explicit context cancels unstarted work and is passed to
  every executor invocation. Executors report cancellation with an error
  wrapping `context.Canceled` or `context.DeadlineExceeded`; unrelated errors
  remain failures even when cancellation happens concurrently.
- The in-memory result contains ordered node and output summaries and
  sequence-numbered state transitions. It carries no process-specific fields,
  execution IDs, timestamps, storage, or serialized event payloads.

## Alternatives considered

### Dispatch a new ready node whenever any worker finishes

Pros:

- Uses available capacity more efficiently.

Cons:

- Completion timing can change which nodes start before fail-fast takes effect,
  leading to different terminal node summaries for the same batch of outcomes.

### Run nodes serially

Pros:

- Minimal synchronization and stable order.

Cons:

- Cannot satisfy F0.4's independent-branch overlap requirement.

## Consequences

### Positive

- State changes have one owner and a stable, observable order.
- Existing F0.1-F0.3 graph and binding contracts remain the execution source
  of truth. F0.5 can implement the small executor interface.
- Tests can prove overlap and dependency barriers using channels.

### Negative / trade-offs

- A fast worker may wait for a slower batch peer before more ready work starts.
- The scheduler cannot forcibly terminate an executor that ignores context.
- Callers and executors must treat the graph definition as read-only for the
  duration of a run.

## Compatibility and migration

Schema v0 and existing graph files do not change. The graph binding API gains
only graph-output materialization using already validated references. F0.6 can
consume transition facts when it adds IDs and event serialization; F0.4 does
not set the durable event format.

## Validation

Unit and integration tests cover state transitions, linear and diamond graphs,
barrier-proven overlap, limits of one and two, fail-fast skipping, binding
resolution, output contracts, cancellation, deterministic ordering, and race
safety. The repository gate remains `make check`.
