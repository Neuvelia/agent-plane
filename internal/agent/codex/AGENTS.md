# Installed Codex adapter guidance

The root `AGENTS.md` and parent `internal/agent/AGENTS.md` apply here.
This guide covers the installed Codex CLI adapter. Read F1.2/ADR-0010 and
F1.3/ADR-0011 before changing process, result, Git or progress behavior.

## Find the owner

| Concern | Start here |
| --- | --- |
| Detection, argv/environment, execution and final result parsing | `adapter.go` |
| Private JSONL progress parsing and supported event projection | `adapter.go`, `progress.go` |
| Duplicate-key, UTF-8 and JSON-depth checks | `json.go` |
| Workspace and invocation-relative Git evidence wrappers | `git.go`, `internal/gitstate/snapshot.go` |
| Process group launch, cancellation and cleanup on Darwin/Linux | `process_unix.go` |
| Fail-closed behavior on other platforms | `process_other.go` |
| Neutral adapter contract and graph output conversion | `internal/agent/AGENTS.md` |

## Preserve these contracts

- Codex execution is an explicit CLI choice through `--adapter codex` and
  an absolute `--codex-bin`. `Detect` checks an executable file, parses its
  version response and probes the required global and `exec` flags with
  bounded calls. Unsupported platforms or protocols fail closed. Keep
  structured argv, `--no-daemon`, `--ask-for-approval never`,
  `--sandbox workspace-write`, ignored user config and an ephemeral session.
  Never widen sandbox or filesystem permissions as a fallback. The adapter
  receives task, advisory paths, context references
  and untrusted correction feedback on stdin, not in command arguments.
- `environment_refs` is the only source of child environment variables:
  validate names, require presence, de-duplicate and pass the explicit list
  to the process. Do not forward ambient HOME, PATH, CODEX_HOME, credentials
  or Git configuration by default. The neutral contract does not resolve
  these references; this adapter does. An absolute workspace must resolve
  to the root of an initialized, non-bare Git worktree with HEAD before
  execution. Process execution is supported on Darwin/Linux in a fresh
  process group. On cancellation and ordinary exit, terminate owned group
  members with bounded TERM/KILL and reap handling. Deliberately escaped
  groups are outside this contract; process groups are not a sandbox.
- Keep provider JSONL and stderr private. Each progress record and final
  result is bounded to 1 MiB. `uniqueJSON` rejects duplicate keys, invalid
  UTF-8, excessive nesting and trailing values. A successful final result
  needs exact top-level `summary` and `artifacts` fields, a nonempty summary
  and valid artifact references. A deferred cleanup attempts to remove the
  private result directory after execution. Failed, missing or malformed
  results produce no accepted graph outputs. Raw metadata and usage remain
  absent unless explicitly supported by a later contract.
- Project only supported public JSONL fields into normalized progress;
  unknown or malformed optional records are skipped, while recognized,
  well-formed failure records within the size bound fail the invocation.
  Do not expose reasoning, raw provider errors, command text, tool
  arguments/results or the raw JSONL stream.
  Completed assistant text may produce `text_delta`; it is not a promise of
  token streaming. Legacy static statuses are allowlisted and limited to
  64 KiB per invocation. Runtime applies configured redaction and bounded
  collection after this adapter's projection.
- Take a Git snapshot before the invocation and compare it with the state
  after a valid result. `git.go` delegates workspace and path rules to
  `internal/gitstate`; keep that package as the single owner of snapshot
  semantics. Return sorted, unique paths with `observed` provenance based
  on index/worktree differences, not model claims. Unchanged pre-existing
  dirt is omitted; further edits to it count. Initially ignored contents
  are not hashed, and newly exposing an initially ignored path fails closed
  because its baseline is unknown. These paths are evidence of changes
  during the invocation, not proof of authorship. `allowed_paths` remains
  advisory here; F1.8's separate, opt-in runtime Gate checks scope after
  execution and cannot prevent or roll back writes.
- Keep errors actionable through typed, safe codes and preserve causes for
  callers without emitting raw provider details. Changed-file evidence is
  emitted by runtime only for a successful observed execution. Do not add
  task text, environment values, raw result, private reasoning or raw
  provider paths to events. Route Git-observed paths through runtime's
  configured redaction. Changing the CLI protocol, Git evidence semantics,
  process cleanup or event projection requires the active feature spec and
  an ADR where the durable contract changes.

The main contracts are `docs/features/F1.2-first-coding-agent-adapter.md`,
`docs/adr/0010-codex-process-and-observed-evidence.md`,
`docs/features/F1.3-unified-streaming.md`, and
`docs/adr/0011-bounded-normalized-progress.md`. For scope checks read
`docs/features/F1.8-changed-files-scope-check.md` and
`docs/adr/0016-changed-files-scope-check.md`. The real installed-CLI smoke
evidence for F1.2 is in `docs/validation/F1.2-codex-smoke.md`; ordinary
changes should use the deterministic fake-CLI and temporary-Git tests first.

## Choose regression tests

- Detection, argv/environment, result boundaries, Git evidence and ignore
  transitions: `adapter_test.go`.
- Owned child cancellation, normal-exit cleanup and process observation:
  `process_test.go`.
- Supported normalized records, malformed optional progress and safe
  failures: `progress_test.go`.
- Shared snapshot behavior: `internal/gitstate/snapshot_test.go`;
  neutral output/correlation behavior: `internal/agent/executor_test.go`,
  `internal/agent/integration_test.go`.
- CLI opt-in and Shell compatibility: `cmd/agentplane/adapter_test.go`.

Run `go test ./internal/agent/codex/...` before `make check`. For process,
concurrency or callback changes, also run the focused race suite:
`go test -race ./internal/agent/codex/...`, plus affected Git/Agent/CLI
tests. Update this guide when a named owner or contract changes.
