# Shell executor guidance

The root `AGENTS.md` applies here. This file guides changes to
`internal/shell/`. The package runs an explicitly configured local process;
runtime owns scheduling, accepted node values, loops and event emission.

## Find the owner

| Concern | Start here |
| --- | --- |
| Shell configuration, input-to-environment mapping, process lifecycle, output ports and typed failures | `executor.go` |
| Shell unit and process-lifecycle tests | `executor_test.go` |
| Graph bindings and declared port types | `internal/graph/AGENTS.md` |
| Scheduling, log redaction, attempt IDs and acceptance | `internal/runtime/AGENTS.md` |
| CLI executor composition and run validation | `cmd/agentplane/AGENTS.md` |

## Preserve these contracts

- A Shell node requires a nonempty `command`. `args` are literal argv values;
  neither command nor args are interpolated from bound inputs or passed through
  an implicit shell. An author may explicitly choose a shell executable. A
  missing `cwd` uses the runner directory; a relative `cwd` is resolved from
  that directory at execution time, not from the graph file. Bare command
  names use the runner's PATH. Reject unknown config fields and malformed
  values before starting a process. `ValidateNodeConfig` checks static config
  without execution.
- The child environment starts empty except for Go's platform-required
  Windows `SYSTEMROOT`. `env_allowlist` copies named, present runner values;
  `env` supplies explicit strings and overrides an identical allowlisted
  name. Only `input_env` maps resolved, declared string inputs to child
  variables. An absent optional input adds nothing; a missing required mapped
  input, wrong runtime type or NUL value fails before launch. Reject
  undeclared/non-string ports, invalid or duplicate target names and collisions
  with `env`/`env_allowlist` without regard to case. Do not embed secrets in
  graph config or assume an input mapping marks a value for redaction.
- `secret_env` maps a child variable to the NAME of a declared secret (F3.8);
  only names are parsed, never values, and errors never quote a secret name.
  Invalid names, duplicates and collisions with `env`/`env_allowlist`/
  `input_env` are rejected without regard to case, mirroring the graph
  validator. Values arrive only through `Request.Secrets` (`Run`, C4.4):
  every configured variable needs a grant, otherwise `execute` fails with
  `FailureConfig` (`errSecretNotGranted`, naming no variable) before starting
  the process. Granted variables are appended to the child environment; a
  grant for a variable the node does not configure is ignored.
  `ExecuteNodeAttempt` is a wrapper that grants nothing.
- `exec.Command` with explicit argv, cwd and environment starts the child through
  `internal/proctree`, which owns its process tree. When the context is done,
  a goroutine joined before `execute` returns calls `Terminate` (request, grace,
  force). Cancellation is classified from that record, not the exit status:
  `FailureCanceled` when termination succeeded, `FailureShutdown` when the tree
  could not be emptied; `ExecutionError.ProcessTermination()` returns the
  outcome. `Executor.Policy` tunes the grace. A descendant that left the group
  is out of scope; Windows terminates only the direct process until Job
  Objects land. It does not sandbox filesystem access. `WaitDelay` bounds the
  drain of inherited stdout/stderr pipes to one second. An otherwise
  successful child with pipes still open yields an output failure with partial
  evidence; a nonzero direct-child exit remains an exit failure. Preserve the
  start-error versus cancellation distinction when they race.
- `Execute` returns raw `Result`: separate complete stdout/stderr capture,
  exit code and start/end timestamps. `ExecutionError` retains that result and
  unwraps its cause for config, start, exit, cancellation and output failures.
  Its raw captured streams are not automatically redacted and are currently
  unbounded in memory. A failed process publishes no accepted graph outputs.
  Only declared `stdout`, `stderr`, `exit_code` and optional `json` outputs may
  be returned. `json` requires `stdout_json: true` and the entire stdout must
  be one valid JSON value; arbitrary surrounding prose is not parsed.
- The log observer receives stdout/stderr in chunks of at most 16 KiB and
  may be called concurrently from the two output-copy goroutines. Runtime
  owns configured secret redaction, event ordering/identity, retention and
  live delivery. Do not treat raw `Result` capture and redacted `log_chunk`
  events as the same storage boundary. Successful declared `stdout`, `stderr`
  and `json` node outputs also retain raw process content in the run result
  and may appear in CLI JSON; log redaction does not sanitize them. A process
  or output error still leaves available evidence in the typed error; runtime
  emits observed logs before terminal lifecycle facts.
- A bounded Shell loop uses the runtime's embedded Gate and attempt lifecycle.
  `ExecuteNodeAttempt` adds only `AGENTPLANE_ATTEMPT=<1-based number>` to a
  loop attempt's child environment. Nonloop execution adds no such variable;
  every Shell node reserves that name case-insensitively in `env`,
  `env_allowlist` and `input_env`. The command is rerun unchanged after a
  valid red Gate result; nonzero exit and other operational failures are
  fatal, not retry signals. Runtime withholds a rejected attempt's output,
  preserves accepted siblings and stops at the configured attempt budget.
  Rejected attempts may leave workspace side effects; this is not isolation
  or rollback.

Read `docs/features/F0.5-shell-node.md`,
`docs/adr/0005-shell-node-execution-contract.md`,
`docs/adr/0008-shell-input-environment-binding.md`,
`docs/features/F1.6a-shell-loop.md` and
`docs/adr/0018-shell-node-correction-loop.md` before changing Shell behavior.
Use `docs/graph-spec.md` for the graph-facing config and output contract.
Changes to process trust, graph config, outputs or retry semantics need a
feature spec and, where root policy requires it, an ADR.

## Choose regression tests

- Config, argv, cwd, environment and input mapping: `executor_test.go`.
- Start/exit/output failure evidence, cancellation, inherited pipes and log
  chunks: `executor_test.go`.
- Loop attempt injection, reserved variable and CLI red/green/exhaustion:
  `executor_test.go`, `cmd/agentplane/shell_loop_test.go`.
- Scheduler acceptance and log-event order: `executor_test.go`,
  `internal/runtime/loop_test.go`, `cmd/agentplane/integration_test.go`.

Run `go test ./internal/shell/...` before `make check`. For process-lifecycle,
streaming or loop changes, also run the affected runtime and CLI tests plus a
focused race check. Update this guide when a named owner or contract changes.
