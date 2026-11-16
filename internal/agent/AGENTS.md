# Provider-neutral Agent guidance

The root `AGENTS.md` applies here, including in `internal/agent/codex/`.
This file adds rules for the provider-neutral Agent contract and executor.
Read the concrete adapter's own code and ADR before changing its process
behavior.

## Find the owner

| Concern | Start here |
| --- | --- |
| Adapter identity, capabilities, typed input/output and optional progress | `contract.go` |
| Explicit adapter lookup, detection, dispatch and graph output conversion | `executor.go` |
| Agent node config, task source, workspace and output-port validation | `config.go` |
| Typed failure categories and safe diagnostic codes | `errors.go` |
| Reusable conformance fixture and deterministic mock | `adaptertest/contract.go`, `adaptertest/mock.go` |
| Installed Codex process, Git evidence and progress translation | `codex/` |
| Runtime scheduling, accepted outputs, feedback and event emission | `internal/runtime/AGENTS.md` |

## Preserve these contracts

- `AgentAdapter` has stable `ID`, `Detect(ctx)` and context-cancellable
  `Execute(ctx, input)`. `Executor.Adapters` is an explicit per-caller map;
  reject a missing adapter or one registered under a different logical ID.
  Keep provider branches out of `internal/runtime` and avoid a global or
  dynamic registry. Unknown optional capabilities stay nil, distinct from
  known false. A loop requires an affirmative correction-feedback capability
  at preflight; a known lack of workspace support is rejected. Do not assume
  untested capability flags are enforced by this generic executor.
- An Agent node needs a nonempty adapter ID, absolute local workspace and
  exactly one task source: nonempty `config.task` or a declared string
  `task` input resolved through a graph binding. Reject unknown config keys,
  wrong types and unsupported output ports before calling the adapter.
  `allowed_paths`, `context_refs` and `environment_refs` are explicit
  references in the neutral contract. This package does not read referenced
  files, resolve secrets, copy the parent environment or isolate writes.
  `allowed_paths` is advisory; an opt-in F1.8 Gate can check Git-observed
  changes after execution, but that is not a sandbox or rollback.
- The runtime owns run, node-run and attempt IDs. Pass correlation and a
  detached correction feedback value to the adapter; do not mint or replace
  IDs. Prefer `NormalizedProgressAdapter` when a normalized observer is
  available, then the legacy `ProgressAdapter`, then base `Execute`.
  Progress sent into runtime must remain observable, bounded and free of
  private reasoning; provider-specific records stay inside the adapter.
- Validate normalized output shape before publishing graph values: changed
  files require a nonempty path and `reported` or `observed` provenance;
  artifact references need a nonempty `ref`; raw metadata must be valid JSON.
  Publish only declared output ports. Nil changed-files/artifacts/usage
  means unknown or absent, while explicit empty lists or an empty usage
  record remain known values. Do not turn unknown usage into zero or
  interpret provider metadata in the runtime. When an observed successful
  execution returns a nonnil changed-files list, its evidence is separate
  from declared graph ports; the runtime decides when to commit and emit it.
- Preserve `ExecutionError` categories and wrapped causes for callers, while
  exposing only finite diagnostic codes in lifecycle events. Do not put task
  text, environment values, raw metadata, provider errors or full outputs
  into events. A failed invocation publishes no accepted graph outputs.
  Concrete adapters own process termination, safe observation and any
  provider-specific environment resolution.

Read `docs/features/F1.1-agent-adapter-contract.md`,
`docs/adr/0009-agent-adapter-and-agent-node-contract.md`, and
`docs/graph-spec.md` for the neutral contract. For current optional progress
and correction feedback read `docs/features/F1.3-unified-streaming.md`,
`docs/adr/0011-bounded-normalized-progress.md`,
`docs/features/F1.6-correction-loop.md` and
`docs/adr/0014-bounded-node-correction-loop.md`. For the installed Codex
adapter read `docs/features/F1.2-first-coding-agent-adapter.md` and
`docs/adr/0010-codex-process-and-observed-evidence.md`. Current CLI/API
static validation calls `Executor.ValidateNodeConfig`; older F1.1 text that
places every config error only at run start predates that shared path. A
change to the adapter contract or graph-visible output shape needs a feature
spec and ADR under the root policy.

## Choose regression tests

- Public adapter semantics, unknown capabilities and mock conformance:
  `contract_test.go`, `adaptertest/contract.go`.
- Config, lookup, output-port, error and cancellation boundaries:
  `boundary_test.go`, `executor_test.go`.
- Runtime dispatch, correlation, data bindings, sibling failure and safe
  events: `integration_test.go`.
- Concrete Codex process, Git baselines and progress: `codex/adapter_test.go`,
  `codex/process_test.go`, `codex/progress_test.go`.

Run `go test ./internal/agent/...` before `make check`. For changes to
cancellation, progress callbacks or shared state, also run
`go test -race ./internal/agent/...` and the affected runtime/CLI tests.
Update this guide when a named owner or contract changes.
