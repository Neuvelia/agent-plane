# CLI and sidecar composition guidance

The root `AGENTS.md` applies here. This file adds rules for
`cmd/agentplane/` source and tests. This package assembles the Go engine,
adapters, store and local API, then presents their results. It does not own
graph semantics, scheduler transitions or event schemas.

## Find the owner

| Concern | Start here |
| --- | --- |
| Process entry, signals and broken pipes | `main.go` |
| Command routing, `run` flags, preflight and exit codes | `cli.go` |
| Shared CLI/API static diagnostics | `validation.go` |
| Typed `--input` and named-environment redaction values | `inputs.go` |
| Human and watch rendering | `output.go` |
| CLI JSON schema and typed output conversion | `json.go` |
| Concrete Shell, Gate and Agent executor composition | `adapters.go`, `cli.go`, `serve.go` |
| `--worktree-root`/`--retain-worktrees` and the Git `WorkspaceManager` adapter (F3.1) | `workspaces.go` |
| Local or interactive human decisions | `approvals.go` |
| `runs list/show` and stored-run replay | `runs.go` |
| `runs prune` and retention at `serve` start (F3.2) | `prune.go` |
| Loopback server setup, persistence and start callback | `serve.go` |

## Preserve these contracts

- Keep the CLI as a caller of `graph`, `runtime`, `events`, concrete
  executors and `store`. `graph validate` and API/document validation use
  the same side-effect-free `validateGraph` path: graph rules first, then
  Shell, Gate and Agent config checks in node-ID order. Preserve diagnostic
  codes, locations and order, including `E_NODE_CONFIG_INVALID`. `run`
  validates graph, bindings and typed inputs before execution; runtime
  preflight remains the authority for execution capability. An invalid
  graph must not start a process.
- Preserve exit codes 0 success, 1 internal or presentation failure, 2
  usage, 3 graph/preflight error, 4 started-run failure and 130 signal
  cancellation. `main.go` turns SIGINT/SIGTERM into context cancellation
  and ignores SIGPIPE so closed output pipes become reportable write errors.
  `--json` emits one final `cli_schema_version: 1` document; it cannot be
  combined with `--watch`. Keep human text off JSON stdout, typed outputs
  explicit, and empty diagnostics/output collections stable. The renderer
  consumes events; it must not infer a second execution state machine.
- `--watch` consumes a bounded live prefix while work runs, then replays
  retained events with sequence greater than the last delivered event.
  Preserve no-duplicate tail rendering and explicit progress-omission
  evidence. A failed live output writer cancels the run and drains events;
  an externally cancelled run must finish even if output remains blocked.
  Current delivery is bounded and asynchronous under ADR-0011; the older
  synchronous delivery description in ADR-0007 is historical.
- `--input` preserves declared graph value types. `--redact-env NAME`
  reads a set local environment variable and passes its non-empty value to
  runtime redaction without placing it in argv. With `--store-dir`, refuse a
  graph whose raw bytes or parsed strings contain such a value before starting
  the run; the store snapshots exact graph bytes and does not redact them.
  `run` stores history only when opted in, whereas `serve` stores runs in
  `.agentplane/runs` by default. A CLI persistence failure still presents
  the run result but exits 1; a served-run storage failure cancels the run
  and stops forwarding events to clients.
- Keep `serve` bound to loopback with the one-line JSON port/token handshake
  on stdout; do not log or put the token in a URL. The API package owns
  request authentication, route checks, replay and decisions. This package
  composes the store sink before client delivery: `Writer.Append` succeeds
  before forwarding, and a failed append is never forwarded. Progress/log
  writes may still be lost after process kill before their next sync; see
  `internal/store/AGENTS.md`.
  `serve` exits when stdin closes and cancels active runs. `run` selects
  Codex only with `--adapter codex --codex-bin`; `serve` has no Agent adapter
  or `--redact-env` flag in this slice, so Agent nodes cannot execute through
  the server. Preserve the narrow Electron/API boundary in the owning
  packages when changing this composition.

- Workspace isolation (F3.1) is composed only here: `workspaces.go` adapts
  `internal/worktree` to the runtime port and builds it only for a graph that
  sets `isolation: worktree`, so other graphs do no Git or directory work. The
  worktree root defaults to the user cache directory, must be absolute and is
  created only after source preflight passes. `--json` gains an optional
  `workspace_isolation` object (absent without isolation; envelope version
  stays 1); human output states the base commit, refs, retained paths and that
  a worktree is not a sandbox. `serve` accepts the same flags, adds no endpoint,
  and cannot run Agent nodes in this slice, so an isolated graph stops at the
  Agent preflight there.

Read `docs/features/F0.7-cli-runner.md`,
`docs/adr/0007-cli-runner-contract.md`,
`docs/features/F1.3-unified-streaming.md`,
`docs/adr/0011-bounded-normalized-progress.md`,
`docs/features/F2.9-run-store.md`,
`docs/features/F2.5-live-map.md`, and
`docs/adr/0019-local-run-store-and-loopback-api.md` for the command and
delivery contracts. For current validation parity read
`docs/features/F2.4-graph-validation-ui.md` and
`docs/adr/0021-local-graph-document-api-and-write-trust.md`. Read the
relevant feature spec and ADR when changing an Agent, Gate, loop or human
decision path. A CLI JSON, exit-code, storage or serve trust-boundary change
needs a feature spec and, where required by root policy, an ADR.

## Choose regression tests

- Routing, errors, JSON shape, redaction and typed inputs: `cli_test.go`,
  `inputs_test.go`, `integration_test.go`.
- Shared diagnostics and API parity: `validation_test.go`,
  `parity_corpus_test.go`.
- Watch ordering, progress omission, broken pipes and cancellation:
  `output_test.go`, `progress_test.go`, `integration_test.go`.
- Store opt-in, replay, secrets and failure precedence: `store_test.go`.
- `kill -9` of a running CLI (durable prefix, live `running` then `interrupted`,
  lock not inherited by node processes) and persist-before-deliver across
  processes: `crash_test.go` (helper processes `TestCrashHelperProcess`,
  `TestCrashEventsHelperProcess`).
- `runs prune`, its exit codes and the serve-start retention: `prune_test.go`.
- Worktree isolation flags, JSON/human result, exit codes and the
  no-isolation golden guard: `isolation_test.go`.
- Server handshake, persistence, invalid graphs and shutdown:
  `serve_test.go`.
- Executor selection and human prompts: `adapter_test.go`,
  `approvals_test.go`, `human_test.go`.

Run `go test ./cmd/agentplane/...` before `make check`. For process lifecycle,
streaming or cancellation changes, also run the relevant built-binary and
cross-package tests and a race check. Update this guide when a named owner
or contract changes.

## Attempt diff capture (`--record-diffs`, F2.8b, ADR-0026)

`run` and `serve` accept `--record-diffs` (default off). `attemptdiff.go` is the
only place that composes `internal/adapter/attemptdiff` behind
`runtime.AttemptDiffer`, and only for graphs with a loop Agent node, so other
graphs open no session and no scratch directory. The flag prints a one-line
notice on stderr; redaction of file content and paths comes from `--redact-env`.
In `serve` the flag is fixed at startup and every run opens its own session.

## Secret delivery (F3.8, ADR-0029)

`secrets.go` is the only place that builds the `env` secret provider
(`envsecrets.Provider{Lookup: os.LookupEnv}`); `run` and `serve` both pass it as
`Options.SecretResolver`, so `serve` reads its own process environment when a
run starts and keeps no resolved value between runs. `executors.go` chooses the
executor for both: a plain Shell graph keeps the bare `shell.Executor{}`, but a
graph with any `secret_env` node, loop or non-Shell node gets `nodeExecutor`,
the only composed executor that hands `ExecutionObservation.SecretEnv` to Shell
(`ReceivesSecrets` is true for `shell` only). Known gap, tracked as C4.5: the
accepted outputs shown by the CLI document are not redacted yet.
