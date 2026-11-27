# ADR-0005 - Shell node execution contract

**Status:** accepted; the unbounded-capture consequence is amended by ADR-0045 (accepted)
**Date:** 2026-10-07

## Context

F0.4 has a generic `ExecuteNode` contract and no process implementation.
F0.5 needs a local process executor with evidence for failures while keeping
the scheduler independent of operating-system process details.

## Decision

- `internal/shell.Executor` implements the F0.4 executor interface. It runs
  an explicit executable and literal argv with `exec.CommandContext`; it
  never invokes a shell or interpolates node inputs.
- Child environment is empty by default, apart from Go's platform-required
  `SYSTEMROOT` addition on Windows. An allowlist copies selected runner
  variables, and explicit `env` values override them. This avoids implicit
  secret inheritance and leaves secret references for a later feature.
- stdout and stderr are captured independently in memory. `Result` holds
  both streams, exit code, and start/end timestamps. A typed `ExecutionError`
  distinguishes configuration, process start, nonzero exit, cancellation,
  and JSON output failures. It retains `Result` on failures. Successful graph
  outputs are restricted to declared `stdout`, `stderr`, `exit_code`, and
  optionally `json` ports.
- `stdout_json: true` parses the complete stdout as one JSON value when the
  `json` output is declared. Invalid JSON fails the node.
- Context cancellation kills and waits for the direct child process. Process
  descendants and process-tree termination are outside this decision.
- `exec.Cmd.WaitDelay` allows one second for output pipes to close after the
  direct child exits or cancellation starts. An inherited pipe still open at
  that point yields an output failure with partial captured evidence if the
  direct child otherwise succeeded. A nonzero direct-child exit remains an
  exit failure. This bounds `Wait` without attempting to manage descendants.
- F0.6 can replace the stdout/stderr writers with streaming writers. This
  decision defines no event payload, log store, or streaming behavior.

## Consequences

- The scheduler and generic node result do not gain process-specific fields.
- Failure evidence is available through `errors.As` on the generic runtime
  failure's `Err` field.
- Captured output is currently unbounded in memory; larger log/artifact
  handling belongs to a later feature.
- Shell nodes cannot yet consume bound inputs as process arguments or
  environment values; a future explicit mapping contract is required.

## Validation

Portable Go helper-process tests cover argv fidelity, streams, exit and start
errors, cwd, environment, cancellation, JSON, and F0.4 scheduler integration.
