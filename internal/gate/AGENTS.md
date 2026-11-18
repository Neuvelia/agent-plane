# Deterministic Gate guidance

The root `AGENTS.md` applies here. This file adds rules for `internal/gate/`
source and tests. A Gate evaluates explicit bound inputs and constructs a
deterministic Check Result for runtime's observation callback. Runtime owns
node state, retry and event emission.

## Find the owner

| Concern | Start here |
| --- | --- |
| Gate config, required ports, supported kinds and path rules | `config.go` |
| Verdict execution, predicate and reported-file comparison | `executor.go` |
| Restricted read-only `/usr/bin/diff` comparison | `command.go` |
| Git-observed changed-file scope, warn/deny and evidence budget | `changed_files.go` |
| Shared Git baseline capture and path observation | `internal/gitstate/AGENTS.md` |
| Check Result schema and legacy decoding | `internal/events/AGENTS.md` |
| Observation validation, trace order and loop feedback | `internal/runtime/AGENTS.md` |

## Preserve these contracts

- Gate config requires a bounded nonempty `check_id`, one supported kind,
  declared required input ports and only an optional `verdict:string` output.
  Reject unknown or kind-inappropriate config fields before execution.
  `ValidateNodeConfig` is side-effect free; CLI/API validation calls it
  without running checks. Keep graph edges as real data bindings, not hidden
  ordering or Agent-state access.
- A valid green result succeeds and may publish the declared `verdict`
  output. A valid red result records the Check Result, returns `RedError`,
  publishes no accepted output and blocks dependent work under runtime
  semantics. Config, input, process and cancellation failures are errors,
  never fabricated verdicts. The explicit exception is a non-cancellation
  Git inspection failure in `changed_files_scope` warn mode, represented
  as an indeterminate green result. `Executor` constructs one typed
  Check Result and calls `ExecutionObservation.Check` when available;
  runtime validates, detaches, redacts and emits the fact with scheduler-owned
  run/node/attempt identity before the terminal lifecycle event. Check facts
  survive progress limits.
- `command` is a narrow read-only comparison, not arbitrary shell execution.
  Accept only fixed `/usr/bin/diff` on Darwin/Linux, generated argv over two
  preopened regular non-symlink files under a canonical absolute cwd, and a
  controlled `LC_ALL=C` environment. Exit 0 is green, exit 1 red; other
  exits, start/drain failures and cancellation are operational errors.
  The diff starts through `internal/proctree`: cancellation terminates the
  whole tree (`Executor.Termination` tunes the grace) and a leader that exits
  on its own has its group swept. The whole evidence step is bounded by
  `commandEvidenceBound` (30 s, `Executor.EvidenceBound` in tests); exceeding
  it is an operational error wrapping `context.DeadlineExceeded`, never a red
  verdict. `Executor.diffPath` is a test-only seam.
  Capture/redact bounded stdout and stderr with omitted-byte counts. Path
  preflight does not protect against hostile concurrent filesystem mutation
  and is not a general process sandbox.
- `json_predicate` reads a required JSON object input, follows literal
  object keys and compares a scalar/null with `eq` or `ne`. A missing field
  or predicate mismatch is a red result; malformed JSON, wrong traversal
  type or invalid config is an operational error. Use `expected_json` for
  exact decimal/exponent or large integer literals; do not introduce
  float64 rounding or a general expression evaluator.
- `reported_file_scope` checks an explicitly bound JSON list of reported
  paths against exact files or directory prefixes. It normalizes portable
  relative paths, sorts/deduplicates violations and labels evidence
  `reported`. Malformed records fail; out-of-scope records are red. This
  checks supplied data, not filesystem changes or provider honesty.
- `changed_files_scope` uses a runtime-captured Git baseline and a required
  bound candidate input; the candidate value is a data dependency, not the
  source of changed-file paths. The Gate's workspace and allowed rules must
  match its Agent declaration under the loop contract. `CheckChangedFiles`
  compares Git-observed paths with normalized exact-file/directory rules.
  Out-of-scope changes in `warn` mode are green with an explicit violation
  reason; in `deny` mode they are red. A non-cancellation Git inspection
  failure is an explicitly indeterminate green result in `warn`, but an
  operational error in `deny`. Evidence is bounded with an omitted count.
  Neither mode prevents writes, rolls them back or proves which process
  authored them.
- Keep CheckResult v1 scope as the sorted checked input port names, separate
  from allowed filesystem paths and original correction scope. Preserve
  nonnegative duration, typed evidence, encoded-size limits, redaction and
  distinction between absent and unavailable references. Do not manufacture
  missing fields for historical schema-4 Gate facts. A change to verdict,
  event shape, scope semantics or read-only process trust needs a feature
  spec and ADR under root policy.

Read `docs/features/F1.4-gate-node.md`,
`docs/adr/0012-gate-node-contract.md`,
`docs/features/F1.5-check-result-contract.md`,
`docs/adr/0013-check-result-contract.md`,
`docs/features/F1.8-changed-files-scope-check.md`, and
`docs/adr/0016-changed-files-scope-check.md`. For embedded correction
behavior read `docs/features/F1.6-correction-loop.md` and
`docs/adr/0014-bounded-node-correction-loop.md`.

## Choose regression tests

- Config, exact predicate numbers, reported paths, fixed diff process,
  cancellation and Check Result integration: `executor_test.go`; whole-tree
  termination and the evidence bound: `command_tree_test.go`.
- Git path classes, scope rules, warn/deny, ignored files and long-path
  evidence: `changed_files_test.go`.
- Observation consistency, red downstream blocking and loop feedback:
  `internal/runtime/gate_test.go`, `internal/runtime/loop_test.go`,
  `internal/runtime/changed_scope_integration_test.go`.
- Event shape and legacy compatibility: `internal/events/gate_test.go`,
  `internal/events/check_result_test.go`; CLI path:
  `cmd/agentplane/gate_test.go`.

Run `go test ./internal/gate/...` before `make check`. For a change to
command lifecycle, Gate observations or correction cycles, also run the
affected runtime, events and CLI tests plus a focused race check. Update
this guide when a named owner or contract changes.
