# Installed Claude Code adapter guidance

The root `AGENTS.md` and parent `internal/agent/AGENTS.md` apply here.
This guide covers the installed Claude Code CLI adapter (`claude -p`, feature
F1.2b). Read the F1.2b plan/spec, ADR-0009/0010 (the Codex contract it mirrors)
and F1.3/ADR-0011 before changing process, result, Git or progress behavior.

**Unverified protocol.** The protocol was designed against the plan, not a
run of the real CLI (none was available when this package was written). Every
flag, record shape and variable name taken from the plan is marked
`ASSUMED(spike)` in a doc comment. Grep for that marker when the spike
(plan step 0) runs and remove it only for details it confirmed.

## Find the owner

| Concern | Start here |
| --- | --- |
| Adapter, safe `Error`/diagnostic codes, execute flow | `adapter.go` |
| Detection: absolute binary, `--version`, `--help` flags, bounded probes | `detect.go` |
| argv, `environment_refs`, fixed adapter env, private HOME/config dir | `env.go` |
| Stdin prompt layout (task, advisory scope, refs, untrusted feedback) | `prompt.go` |
| Final `structured_output` validation | `result.go` |
| Bounded stream-json reader and projection to normalized events | `progress.go` |
| `Workspaces` Git evidence port (implemented over `internal/gitstate` in `cmd/agentplane/claudeworkspaces.go`) | `git.go` |
| Process launch through `internal/agent/cliproc` and error mapping | `process.go` |

## Preserve these contracts

- Execution is an explicit CLI choice and an absolute binary. `Detect` checks
  an executable file, the `--version` response and the required flags in
  `--help` with 10 s bounded probes and an empty environment; Darwin/Linux
  only, otherwise fail closed. `UsageReporting` is false; usage and raw
  metadata stay nil (plan D9). Missing binary is "unavailable", not an error.
- argv is fixed (`arguments()`): stream-json with `--verbose`, `--json-schema`,
  `--no-session-persistence`, `acceptEdits` with `--permission-prompts none`,
  `--tools Read,Edit,Write,Glob,Grep`, `--strict-mcp-config`,
  `--disable-slash-commands`, `--bare`. Bash is NOT enabled (plan D4 fallback a:
  Claude Code has no OS sandbox by default, so it is weaker than Codex
  `workspace-write`; file tools only until a sandbox is confirmed). Never add
  `bypassPermissions`, `--dangerously-skip-permissions`,
  `--allow-dangerously-skip-permissions` or `--bg`, and never widen `--tools`
  as a fallback. `TestArgvAndEnvironment` pins this. The task, advisory paths,
  context references and untrusted feedback go on stdin, never argv.
- The child environment is only `environment_refs` values plus fixed adapter
  constants (telemetry/updater switches, private `HOME`, `CLAUDE_CONFIG_DIR`).
  Refs may not name an adapter-owned variable. Auth is `ANTHROPIC_API_KEY`
  through `environment_refs` (plan D6). The private HOME is a fresh temp
  directory verified to be outside the workspace and removed after the run, so
  CLI config writes cannot reach the workspace or Git evidence (plan D5).
  `cmd.Dir` is the canonical workspace (no `--cd` equivalent exists).
- Process tree, cancellation and cleanup are owned by `cliproc.Run` over
  `internal/proctree`; `claude_cancelled` when the tree was emptied,
  `claude_shutdown` when it could not be. Stderr is discarded by `cliproc`.
- Diagnostic codes: `claude_detection`, `claude_unsupported_platform`,
  `claude_unsupported_capability`, `claude_environment`, `claude_workspace`,
  `claude_git`, `claude_start`, `claude_shutdown`, `claude_cancelled`,
  `claude_process_exit`, `claude_result`, `claude_progress`. Never put raw
  stderr, API error text, tool content or task text in an error or event.
- Stream records are private. A record larger than 1 MiB is skipped without
  being buffered (unlike Codex, where size fails closed), because tool
  results can be large. A skipped record can never be the result, so an
  oversized, missing, duplicate, invalid or error `result` yields
  `claude_result` and no accepted outputs; an oversized record after a valid
  result is rejected as ambiguous. Invalid, duplicate-key or unknown optional
  records are skipped. A successful result needs `subtype: success`,
  no `is_error`, and `structured_output` with exactly `summary` (nonempty)
  and `artifacts` (nonempty `ref`, optional `name`), checked with
  `cliproc.UniqueJSON`.
- Projection (`progress.go`) adds no event types: `system/init` ->
  `status session_started`; completed assistant `text` block -> `text_delta`
  (not token streaming); `thinking` skipped; `Bash` tool_use/result ->
  `command_started/finished` (no command text, exit code nil); `Edit`/`Write`/
  `MultiEdit`/`NotebookEdit` + successful result -> `file_changed` with
  `reported` provenance only when the path is confirmed inside the workspace
  (action is always `update`); other tools -> `tool_call_started/finished`
  (name and status only); `result` -> `status turn_completed`. Tool inputs,
  tool results, thinking and unknown records never cross this boundary.
- Evidence is Git-derived, as for Codex: snapshot before, snapshot after a
  valid result, sorted unique `observed` paths via the `Workspaces` port (backed by `internal/gitstate` in the composition root; this package imports no other adapter). A path
  the model reports is never evidence. Initially ignored content is not
  hashed; newly exposing an ignored path fails closed (`claude_git`).

## Choose regression tests

- Detection, argv/environment, result boundaries, Git evidence, private
  config dir, feedback on stdin: `adapter_test.go`.
- Stream reader, projection, oversized-record handling: `progress_test.go`.
- Owned child cancellation and ordinary-exit cleanup: `process_test.go`.
- Shared snapshot behavior: `internal/gitstate`; process helpers:
  `internal/agent/cliproc`.

All tests use a fake POSIX shell CLI; none uses the real `claude` or an API
key. Run `go test ./internal/agent/claudecode/...` and
`go test -race ./internal/agent/claudecode/...` (Darwin/Linux for the process
tests) before `make check`. Update this guide when a named owner or contract
changes.
