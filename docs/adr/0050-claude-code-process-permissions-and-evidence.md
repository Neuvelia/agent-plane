# ADR-0050 - Claude Code adapter: process permissions and evidence

**Status:** accepted, pending spike (2026-11-27). The owner accepted decisions D1-D9 of the plan. The
protocol assumptions in D3-D6 and the `stream-json` record shapes are **assumed** from `claude --help` of
Claude Code 2.1.332 and have **not** been exercised against a running CLI. They are confirmed or changed by
the spike in `docs/validation/F1.2b-claude-code-spike.md` (NOT RUN); every change lands here as an amendment.
**Date:** 2026-11-27

## Context

ADR-0009 and ADR-0034 fix a provider-neutral Agent adapter interface and say a second adapter must keep that
shape or change it by ADR. ADR-0010 fixes the process, permission and evidence invariants for the only real
adapter, Codex. F1.2 listed a second adapter as P1 until the contract stabilised; F1.1-F1.8 are done, so
F1.2b adds the installed Claude Code CLI (`claude -p`) as the second adapter.

Claude Code differs from Codex in ways that touch the trust boundary: it has no OS sandbox by default
(permissions are enforced by the tool layer), it has no `--cd` (the working directory is the process
directory), it writes configuration under `HOME`/`CLAUDE_CONFIG_DIR`, and its structured stream is not
formally versioned. This ADR records the choices for those points.

## Decision

### D1-D9 (accepted 2026-11-27)

| ID | Decision |
| --- | --- |
| D1 | Registry ID `F1.2b` (`kind: derived`, parent `F1.2`); adapter ID `claude` (matches the ADR-0025 capability row id). |
| D2 | Shared process helpers (`limitedBuffer`, `uniqueJSON`, `runProcess`) move to a neutral package; `claudecode` never imports `codex` (R2). Codex behavior is unchanged. |
| D3 | Config isolation: `--bare` plus `--strict-mcp-config` (no hooks, project `CLAUDE.md`, plugins or keychain). Alternatives kept in reserve: `--setting-sources ""`, `--restricted`. **Assumed, pending spike.** |
| D4 | Bash only with the Claude Code sandbox, fail closed; **until the spike confirms the sandbox, Bash is OFF in v1** (fallback (a): file tools only, `Read,Edit,Write,Glob,Grep`). See "Trust boundary". |
| D5 | A private temporary `HOME`/`CLAUDE_CONFIG_DIR` outside the workspace, removed after the run. **Assumed, pending spike** (writes into the cwd without `HOME` would create false evidence). |
| D6 | Authentication through `ANTHROPIC_API_KEY` named in the node's `environment_refs` (consistent with D3/`--bare` and D5; subscription OAuth needs a real `HOME` and is out of v1). **Assumed, pending spike.** |
| D7 | `--adapter` is repeatable; several adapters may be registered in one run (`agent.Executor.Adapters` is already a map). |
| D8 | The capability row `claude` becomes `agent_adapter` when configured, with desktop texts now (no desktop run of agents; that stays F2.14). |
| D9 | `Usage` and `RawMetadata` are `nil` in v1; cost and tokens are deferred to F3.10. |

### Invocation (assumed protocol)

```text
claude -p
  --output-format stream-json --verbose --input-format text
  --json-schema '<schema {summary, artifacts}>'
  --no-session-persistence
  --permission-mode acceptEdits --permission-prompts none
  --tools "Read,Edit,Write,Glob,Grep"
  --strict-mcp-config --disable-slash-commands
  --bare
```

`cmd.Dir` is the validated workspace. The task, advisory `allowed_paths`, `context_refs` and correction
feedback go on stdin as untrusted data, never in argv. The environment is exactly the named
`environment_refs` values plus adapter constants (for example `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`,
`DISABLE_AUTOUPDATER=1`, a private `HOME`/`CLAUDE_CONFIG_DIR`); the exact variable names are to be confirmed by
the spike. No ambient `HOME`, `PATH` or credentials are copied.

### Codex-to-Claude invariant mapping

| Codex invariant (ADR-0010) | Claude Code |
| --- | --- |
| `--ask-for-approval never` | `--permission-prompts none`: whatever would prompt is denied |
| `--sandbox workspace-write` | no OS sandbox by default; see D4 and "Trust boundary" |
| `--ignore-user-config` | `--bare` (D3); fallbacks `--setting-sources ""` or `--restricted` |
| `--ephemeral` | `--no-session-persistence` |
| `--output-schema` + `--output-last-message` | `--json-schema`; the result is the `structured_output` of the final `result` record |
| `--json` | `--output-format stream-json --verbose` |
| `--no-daemon` | not applicable (no daemon); `--bg` is never used |
| never widen the sandbox | never `bypassPermissions`, `--dangerously-skip-permissions` or `--allow-dangerously-skip-permissions`; WebFetch, WebSearch, MCP and plugins are not enabled |

Detection follows Codex: an absolute executable `Binary`, `--version` (regex
`^([0-9]+\.[0-9]+\.[0-9]+\S*) \(Claude Code\)\s*$`) and `--help` probes for the required flags, 10 seconds each,
each probe in a fresh private `HOME` with the same fixed adapter environment as `Execute` (D5) and no `environment_refs`, Darwin and Linux only (other platforms fail closed, deferred with F3.4a). The **minimum CLI
version is set by the spike**; until then 2.1.332 is the only version the plan was written against and **no minimum is enforced** (the "old
version" test case is an unparseable version string). Because probes see only the adapter environment (no `PATH`), an
npm or script-based `claude` (`#!/usr/bin/env node`) cannot pass `Detect`; the remedy is to install the native binary.

### Result, progress and evidence

- Required result: `structured_output` with exactly a non-empty `summary` and `artifacts` whose `ref` values are
  non-empty, validated with the shared `uniqueJSON`. A missing, malformed, duplicated or oversized `result`
  record fails with `claude_result` and accepts no outputs.
- Progress is a projection of `stream-json` onto the existing F1.3 event types (`status`, `text_delta`,
  `command_started/finished`, `file_changed` with `provenance: reported`, `tool_call_started/finished`).
  **No new event types and no event schema bump.** `thinking` blocks, tool contents, stderr, API error text and
  task text are never projected. Tool `Bash` maps to commands without the command text; an unknown exit code is
  `nil`. Unlike Codex, an oversized **optional** record (for example a `tool_result` from `Read`) is skipped
  while streaming, without buffering it whole; only an oversized or malformed `result` fails closed.
- Changed files come from the Git snapshot before and after the run (`internal/gitstate`, same baseline rules as
  ADR-0010), never from tool-reported paths. A reported path absent from Git is not a changed path.
- `node_evidence` records `adapter_id = "claude"`. New diagnostic codes: `claude_detection`,
  `claude_unsupported_platform`, `claude_unsupported_capability`, `claude_environment`, `claude_workspace`,
  `claude_git`, `claude_start`, `claude_shutdown`, `claude_cancelled`, `claude_process_exit`, `claude_result`,
  `claude_progress` (reserved, not produced yet). Both are additive values of existing fields.
- Cancellation uses `internal/proctree` as for Codex: TERM to the process group, bounded reap and drain.
  Descendants (including background tools) are in the group; deliberately escaped groups are out of contract.

### CLI contract

`agentplane run --adapter claude --claude-bin ABSOLUTE_PATH`. `--adapter` may be repeated (D7), for example
`--adapter codex --codex-bin ... --adapter claude --claude-bin ...`. `serve --claude-bin` is used for detection
only, as `--codex-bin` is. A missing or unregistered adapter error names the adapter and the flags to pass; the
remedy text becomes a per-adapter map in `cmd/agentplane/executors.go`. `--adapter ""` is a usage error and a bare `--codex-bin` fails with `--codex-bin requires --adapter codex`. Graph schema is unchanged:
`adapter: claude` is a value of the existing field. A per-node tool list (`config.tools`) would change the graph
schema and needs its own ADR; it is not part of v1.

### Trust boundary (weaker than Codex)

Codex `workspace-write` is an OS sandbox. Claude Code, by default, enforces permissions in its tool layer only:
a permitted `Bash` command runs with the full privileges of the user and can read and write outside the
workspace and use the network. The Claude boundary is therefore **weaker** than the Codex boundary, and
AgentPlane does not pretend otherwise.

Consequences recorded as decisions:

1. **Bash is OFF in v1** (D4 fallback (a)). The tool set is `Read,Edit,Write,Glob,Grep`, whose paths the CLI
   is assumed to confine to the workspace by permission rules (to be confirmed by the spike). An agent without
   Bash cannot run tests or builds; a Shell/test Gate node after the agent covers that in the graph (F1.4b).
2. Enabling Bash is a later amendment of this ADR and requires the spike to show that the CLI sandbox
   (bubblewrap on Linux) is available, enabled through `--settings`, and **fails closed** when unavailable.
3. Never `bypassPermissions` or any `--dangerously-*` flag; the tests assert their absence from argv.
4. **Git control files.** In `acceptEdits`, `Edit` and `Write` can write `<workspace>/.git/config`,
   `.git/hooks/*` and `.git/info/attributes`. `internal/gitstate` snapshots exclude these, so a write there is
   invisible as evidence, yet the user's next plain `git` command can execute it. With Bash off this is a file
   write leading to code execution outside any sandbox. Mitigation: the adapter hashes `config`,
   `config.worktree`, `info/exclude`, `info/attributes` and `hooks/**` (following a symlinked `hooks`) of the
   shared Git directory and, for a linked worktree, of its own Git directory, before and after the run and returns `claude_git` on any change. A deny rule such as `Edit(.git/**)`/`Write(.git/**)` is
   ASSUMED and listed as a spike item for defense in depth.
5. Network: model traffic is remote by definition (as with Codex); WebFetch, WebSearch, MCP, plugins, telemetry
   and the autoupdater are disabled.

## Alternatives considered

### Alternative A: Bash enabled with the CLI sandbox now (D4 option b)

Pros:

- A useful agent (tests, builds).

Cons:

- Depends on an unconfirmed sandbox; cannot be accepted without the spike. Kept as the target after the spike.

### Alternative B: Bash without sandbox behind an explicit flag (D4 option c)

Pros:

- Simple.

Cons:

- Gives up the process-isolation principle; rejected.

### Alternative C: Copy process helpers into `claudecode`

Pros:

- No refactor of Codex.

Cons:

- Duplicates security-sensitive code (D2); rejected.

### Alternative D: OAuth subscription authentication

Pros:

- No API key.

Cons:

- Needs the user's real `HOME`, contradicting D3/D5; deferred.

## Consequences

### Positive

- Same neutral interface, event vocabulary, evidence model and cancellation semantics as Codex.
- Config files cannot pollute the workspace or the Git evidence (D5).
- The weaker boundary is explicit and the risky tool is off.

### Notes

- The plan's `.agents/graphs/` Claude template was replaced by `docs/examples/claude-agent.yaml`:
  `cmd/agentplane/templates_test.go` (S7 rule and `allowedRequires`) allows only Codex, and a template would need
  that rule changed by an owner decision.
- The desktop keys `cap.meaning.claude.notConfigured` and `foundOnPath` are unreachable from `serve` (without
  `--claude-bin` the row stays `agent_cli`); they are dead until the desktop can configure adapters (F2.14).

### Negative / trade-offs

- v1 agents cannot run commands. Project `CLAUDE.md` is not read (D3).
- A pay-per-token API key is required for the real smoke test.
- `stream-json` is not formally versioned; protocol drift is caught only by fail-closed result validation and
  the minimum version.

## Compatibility and migration

No graph, event schema or interface change. New CLI flags are opt-in; existing Codex and Shell behavior is
unchanged, including the Codex tests after the helper move. The `claude` capability row changes from
`detected_no_adapter` to `agent_adapter` only when configured, which is the transition ADR-0025 anticipates.

## Validation

- Deterministic: fake CLI tests (argv and env, evidence, dirty baseline, private config dir, cancellation with
  a child, oversized records, correction feedback on stdin) mapped in `docs/features/F1.2b-claude-code-adapter.md`.
- Pending spike: `docs/validation/F1.2b-claude-code-spike.md` (NOT RUN) closes D3-D6 and the record shapes.
- Pending real smoke: AC10 (`docs/validation/F1.2b-claude-code-smoke.md`, NOT RUN template). Without it F1.2b
  stays `in_progress` in the roadmap registry (the registry has no `partial` status).
