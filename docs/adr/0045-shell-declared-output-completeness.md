# ADR-0045 - Bounded Shell capture and complete declared outputs

**Status:** proposed
**Date:** 2026-11-27
**Features:** F0.5 (#181); full log as an artifact stays F3.5
**Amends:** ADR-0005 ("captured output is currently unbounded in memory")

## Context

ADR-0005 left Shell stdout and stderr unbounded in memory. F0.5 requirement 10
(#181) bounds the capture: the executor keeps at most 1 MiB per stream, still
drains and streams the rest to the log observer, and ends a truncated text with
an explicit marker `[agentplane: N bytes of stdout omitted; capture limit L
bytes]`.

The retained text also feeds a node's declared output ports. Before this
decision only the `json` port failed on truncation. A declared `stdout` or
`stderr` string port received 1 MiB plus the marker as if that were the real
output. The marker then reached downstream nodes, recorded values and graph
outputs, and a downstream Gate judged corrupted data. An edge is a data
dependency (AGENTS.md rule 4), so a value that is not the process's output must
not travel on it.

## Decision

1. Capture is bounded at 1 MiB per stream, with the omission marker and
   omitted-byte counts in `shell.Result` (`StdoutOmitted`, `StderrOmitted`).
   Streaming to the log observer is unaffected.
2. **A declared output port carries the complete stream or nothing.** When a
   node declares `stdout`, `stderr` or `json` and the capture limit omitted
   bytes of that stream, the node fails with `FailureOutput`. The error wraps
   `shell.ErrOutputTruncated` and names the port and the omitted count. The
   retained text with its marker stays in the failure evidence.
3. A stream that feeds no declared port may be truncated without failing the
   node. Its marker is only in the logs and the failure evidence.

Keeping more than 1 MiB of a declared output requires the artifact store
(F3.5); until then such a node must write to a file and pass a path.

## Alternatives considered

### Keep the full stream for declared ports

Pros: large outputs keep working.
Cons: memory again depends on what the child prints. A runaway process can
exhaust the runner, which is what #181 bounds.

### Pass the truncated text downstream (the earlier PR #194 behavior)

Pros: nothing fails.
Cons: silently corrupts data on an edge and in recorded values; a Gate then
judges text the process never produced.

## Consequences

### Positive

- A downstream node, recorded value or graph output never receives a marker
  as data.
- Memory per Shell node is bounded.

### Negative / trade-offs

- A node that legitimately prints more than 1 MiB to a declared port now fails
  until F3.5 provides artifacts.

## Compatibility and migration

A graph whose Shell node declares `stdout` or `stderr` and prints more than
1 MiB used to succeed (before #181, with unbounded memory). It now fails with
an output failure that names the limit. Graph schema, events and semantic
versions are unchanged.

## Validation

`internal/shell/executor_test.go`:

- `TestExecuteBoundsCapturedOutputWithExplicitOmissionMarker`
- `TestExecuteWithinCaptureLimitHasNoMarker`
- `TestExecuteFailsADeclaredStreamOutputThatWasTruncated`
- `TestExecuteKeepsAnUndeclaredTruncatedStreamOutOfTheOutputs`
- `TestExecuteKeepsOmittedFailureEvidenceBounded`
