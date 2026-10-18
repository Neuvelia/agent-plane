# ADR-0007 - CLI runner output and live event contract

**Status:** accepted
**Date:** 2026-10-18

## Context

F0.7 must expose the Phase 0 engine to terminal users and CI. F0.6 previously
made events available only after `runtime.Run` returned. The F0.7 `--watch`
requirement needs those same facts during execution without making the CLI a
second lifecycle authority.

## Decision

- Keep command parsing and presentation in `cmd/agentplane`; use the standard
  library and compose existing graph, runtime, Shell, and event APIs.
- Set the CLI concurrency default to 2. The runtime's zero-value default of 1
  remains unchanged for other callers.
- Add an optional caller-owned `runtime.Options.LiveEvents` channel. The
  collector sends each sequenced, copied event to it synchronously while
  holding its sequence lock. A caller consumes while `Run` is active, never
  closes the channel during `Run`, and closes it after `Run` returns. Slow
  consumers apply backpressure until context cancellation. On cancellation,
  if an event cannot reach the consumer, live delivery stops so the consumer
  has a sequenced prefix. The CLI replays the missing tail from the completed
  `RunResult.Events`; nil retains F0.6's prior behavior. The completed event
  slice remains the authoritative record.
- Default human mode replays completed events. `--watch` presents the live
  channel. Both use one event renderer; neither derives execution state.
- `--json` emits one versioned final CLI envelope. It is incompatible with
  `--watch`, and no human text is written to JSON stdout. Typed graph output
  values are converted explicitly rather than serializing private runtime
  structures.
- Use process exit codes 0 (success), 1 (internal), 2 (usage), 3 (graph or
  preflight), 4 (started run failure), and 130 (signal cancellation).
- Treat human-output write failures as internal CLI errors (exit 1). The CLI
  ignores SIGPIPE so a closed standard output or error pipe becomes a write
  error. A failed live renderer cancels the run and continues draining events
  so presentation cannot block scheduler shutdown.
- `--redact-env NAME` resolves set environment values locally and passes them
  to F0.6 `LogRedactions`; secret values do not appear in arguments. This
  affects event logs and human log presentation, not declared graph outputs.

## Alternatives considered

### Replay the completed event slice for `--watch`

This required no runtime change, but it would falsely imply live progress.

### Add a general callback or durable event bus

This would expand F0.6's delivery and failure semantics beyond the CLI need.
The channel seam is smaller and has an explicit backpressure rule.

## Consequences

Live rendering can slow execution when the terminal is slow. An external
caller must consume the live channel continuously or the run can block until
context cancellation. The CLI owns its consumer and waits for it before a
normal return. On cancellation it bounds that wait so a slow or blocked output
pipe does not prevent exit; presentation may omit the terminal event if the
renderer does not finish within that grace period.
JSON stays a final document so CI need not implement a stream protocol.

## Compatibility and migration

Graph schema v0, event schema v1, scheduler state transitions, and Shell
execution semantics are unchanged. Existing `runtime.Run` callers that do not
set `LiveEvents` observe the previous behavior. CLI schema v1 is a new public
contract; changes to its field meanings require a version decision.

## Validation

Tests compare live and retained event sequences, verify concurrent branch
presentation and log streams, parse JSON including empty output arrays, check
process exit codes, and send SIGINT and SIGTERM to the built CLI. Tests also
cover cancellation tail replay, a blocked watch writer, human-output write
errors, built-CLI broken pipes and child cancellation, and named environment
value redaction. `go test -race ./...` and
`make check` are required gates.
