# ADR-0039 - Bounded Shell output capture

**Status:** proposed
**Date:** 2026-11-27
**Source:** issue #181, roadmap audit 2026-11-25 (Q10, Q17); amends ADR-0005

## Context

The roadmap scope of F0.5 includes "a log size limit, with the full log
optionally kept as an artifact". The Shell executor bounded only the size of
one streamed chunk (16 KiB). It kept the whole stdout and stderr in unbounded
in-memory buffers for the `Result` evidence and the `stdout`, `stderr` and
`json` output ports, so one noisy process could exhaust memory. ADR-0005
recorded this as a later concern. The streamed `log_chunk` events are already
bounded by the trace collector (`progress_omitted`, ADR-0011); the gap is the
retained capture. There is no artifact store yet (F3.5).

## Decision

1. The Shell executor retains at most `DefaultCaptureLimit` (1 MiB) leading
   bytes of stdout and, separately, of stderr. `Executor.CaptureLimit`
   overrides it in Go; there is no graph field for it.
2. Output beyond the limit is still drained, so the child never blocks on a
   full pipe, and is still passed to the log observer. It is not retained.
3. The omission is explicit: `Result.StdoutOmitted` / `StderrOmitted` count the
   dropped bytes, and the retained text of a truncated stream ends with
   `[agentplane: N bytes of <stream> omitted; capture limit L bytes]`.
4. A declared `stdout`, `stderr` or `json` output whose stream was truncated
   would carry an incomplete value. The node fails with an output failure
   (`CaptureLimitError`) and the fixed diagnostic code
   `E_SHELL_OUTPUT_TRUNCATED` in `node_finished.diagnostic_codes`. No prefix
   is accepted as a graph value. `exit_code` and undeclared streams are
   unaffected.
5. Keeping the complete log as an artifact is deferred to F3.5. F0.5 records
   this as a divergence with `deferred_to: F3.5`.
6. A cut can split a secret: the retained text then ends with a prefix of it
   that whole-value redaction does not match. When the run result is
   presented with a redaction set (F3.8, ADR-0029), the retained text of a
   truncated stream is masked with a cut-aware redactor that also replaces a
   trailing proper prefix of any redacted form with the marker
   (`ExecutionError.RedactedWithCut`, `logRedactor.FlushCut`). Complete
   streams are masked as before. Live `log_chunk` events are unaffected: they
   see the whole stream. The `CaptureLimitError` cause holds no process text
   and stays reachable with `errors.As` in the redacted result.
7. Until this ADR is accepted, F0.5 stays `partial` with issue #181 open; it
   moves to `diverged` only when the owner accepts the decision.

## Alternatives considered

### Pass the truncated text, with the marker, as the output value

Cons: a downstream node would accept an incomplete value as if it were the
process output, breaking "accepted output is correct and immutable" and making
`stdout_json` fail with a misleading JSON error.

This is the approach of the #181 part of PR #194 (marker passed through in
`stdout`, only `json` fails). The two are mutually exclusive; if this ADR is
accepted, the #181 changes in `internal/shell` and `docs/features/F0.5-shell-node.md`
must be dropped from PR #194, together with its `closes #181`.

### Keep the tail instead of the head

Cons: the head is what output ports and JSON need; the live `log_chunk` stream
already shows the end of a log while the node runs. Head-and-tail can be added
later without changing the contract of the counts and the marker.

### Spill the full log to a temporary file

Cons: that is the artifact store (F3.5): retention, paths and cleanup need
their own decision.

## Consequences

### Positive

- Memory per Shell node is bounded (two streams × limit).
- Truncation is visible in evidence, in the error and as a stable code.

### Negative / trade-offs

- A node that declares `stdout` and prints more than 1 MiB now fails instead of
  succeeding. Such graphs must write to a file and pass its path.
- The retained evidence of a long failing log shows its start, not its end.

## Compatibility and migration

No graph or event schema change: `diagnostic_codes` is an open list. Graphs
whose declared stream outputs stay under 1 MiB behave as before.

## Validation

`internal/shell/capture_test.go`: prefix, marker and counts beyond the limit,
the observer receiving every byte, no marker at exactly the limit, the default
limit, the output failure for `stdout`, `stderr` and `json`, success for
outputs that read no truncated stream, and the diagnostic code on
`node_finished` through the scheduler, and a secret cut by the limit masked in
the presented run result (`TestSecretCutByTheCaptureLimitIsMaskedInTheRunResult`).
`internal/shell/redacted_test.go` and `internal/runtime/log_redactor_test.go`,
`result_redaction_test.go` cover the cut-aware masking on its own.
