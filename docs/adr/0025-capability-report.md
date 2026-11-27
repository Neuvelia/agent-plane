# ADR-0025 - Read-only capability report on the loopback API

**Status:** accepted (macOS native verification pending, #24)
**Date:** 2026-11-16

## Context

F2.11 shows a new user what their machine can run: the engine version, the Git binary the engine uses and
each coding-agent adapter the engine has. Today these facts appear only as a failed run (`codex_detection`,
`codex_git`, "loop adapter is unavailable"). The renderer is sandboxed and must not run processes or see
paths (ADR-0019, ADR-0021), so the facts must come from the Go sidecar. Adding a route is a durable change to
the loopback API contract, and detection starts processes, which AGENTS.md treats as untrusted-input
territory.

Verified facts that bound the decision:

- The engine runs the Git found on `PATH` (`gitlocate.ResolveGit`; see the amendment at the end).
- Codex detection already exists as `codex.Adapter.Detect` (ADR-0009, ADR-0010): an absolute binary,
  `--version`, `--help` and `exec --help` probes with an empty environment, 10 seconds each, Linux and macOS
  only.
- `serve` has no agent adapter; agent nodes cannot run from the desktop. No Claude Code adapter exists.
- Owner decision 2026-11-16: the welcome screen also lists the common agent CLIs for which no adapter
  exists yet (Claude Code `claude`, Cursor Agent `cursor-agent`, Gemini CLI `gemini`), so a person sees
  that AgentPlane noticed them and why it cannot run them. They are found without executing anything.

## Decision

1. **Routes.** The loopback API adds `GET /capabilities` (the last report, detecting first if none exists in
   this process) and `POST /capabilities/check` (empty body or `{}`; detects again). Both sit behind the
   existing bearer-token and `Host` checks, write no CORS headers, bound the body to 64 KiB and are
   registered by exact method and path. Without a configured detector they answer 503.
2. **DTO, version 1.** `{version, checked_at, engine: {version, os, arch}, tools: [{id, kind, status,
   version, reason}]}`. `tools` is `git` followed by the composed adapters sorted by id (today exactly
   `codex`), followed by the agent-CLI table in its fixed order (today `claude`, `cursor-agent`,
   `gemini`). `kind` is `tool`, `agent_adapter` or `agent_cli`. `status` is `available`, `missing`,
   `not_configured`, `unsupported_platform`, `unsupported_version`, `detected_no_adapter` or `error`;
   `reason` is a closed set of codes. `version` is set only for `available` and must match
   `^[0-9A-Za-z.+-]{1,64}$`. The report never contains a path, an environment value, error text or raw
   probe output. Adding the CLI rows, the `agent_cli` kind, the `detected_no_adapter` status and the
   `found_on_path` reason is additive within version 1: clients already show an unknown status verbatim.
3. **Detection is the engine's own.** Git: on Linux/Darwin only, resolve Git as the engine does (`gitlocate.ResolveGit`), stat it, then one bounded
   `--version` probe with structured argv, the environment `GIT_CONFIG_NOSYSTEM=1`,
   `GIT_CONFIG_GLOBAL=/dev/null`, `LC_ALL=C` and nothing else, stdin closed, the temp directory as working
   directory, output capped at 4 KiB, a 10 second timeout with termination. Adapters: the existing
   `Detect`, with the error kind (never its text) selecting the status. On other operating systems no probe
   process starts and the Git and adapter rows are `unsupported_platform`. Adapters are never
   looked up on `PATH` for execution; Git is resolved as the engine does (see the amendment at the end).
   On Darwin, if the resolved path is `/usr/bin/git`, it is an `xcrun` shim that opens the "install developer tools" dialog when no developer
   tools exist, so it is executed only after a stat (no execution) finds a real Git under the Command Line
   Tools, the `xcode_select_link` target or the default Xcode location; otherwise the row is `missing`
   with `developer_tools_missing`. This is unit-tested with injected stat/readlink and still needs a
   native macOS check.
4. **Codex binary.** `serve` accepts an optional `--codex-bin ABSOLUTE_PATH` used only to compose detection;
   agent execution in `serve` is unchanged. Without it Codex is `not_configured` and `Detect` is not called.
   Electron main passes it only from an absolute `AGENTPLANE_CODEX_BIN`. When it is absent, a stat-only
   `PATH` lookup of `codex` (see 4a) turns the row's reason into `found_on_path`, so the UI can say "found
   on PATH; set AGENTPLANE_CODEX_BIN to use it". The found path is not reported and not executed.
4a. **Agent CLIs without an adapter (owner decision 2026-11-16).** A small fixed table in
   `internal/capability` names each CLI by report id and command name (`claude`/`claude`,
   `cursor-agent`/`cursor-agent`, `gemini`/`gemini`); adding one is a one-line change plus its UI text.
   Each is looked up on the sidecar's `PATH` with `exec.LookPath`, which inspects the filesystem (stat and
   an access check) and never runs the file. Found is `detected_no_adapter`, otherwise `missing`. These
   files are never executed, their configuration is never read and the found path never leaves the
   lookup. The lookup works on every OS, so these rows are shown on Windows too. The lookup function is
   injected so tests use fakes, and a test with real `exec.LookPath` and marker-writing scripts proves no
   process is started. The `PATH` seen is the sidecar's own; a desktop app started from a macOS GUI
   inherits a short `PATH`, so a CLI installed elsewhere can show as `missing`.
5. **Single flight, memory only.** One detection runs at a time; concurrent callers join it. The shared
   detection is bounded by the server context, not by one client's request. The report is kept in memory and
   discarded when the sidecar exits. Detection emits no run event and writes nothing to the store or disk.
6. **Package boundary.** Detection lives in `internal/capability`, which depends on `internal/agent` but on
   no network package; `internal/api` only serves the report; `cmd/agentplane/serve.go` composes it.

## Alternatives considered

### Electron main runs the probes

Pros:

- No new sidecar route.

Cons:

- Duplicates the engine's detection in JavaScript and can disagree with what the engine will actually use
  (fixed Git path, Codex flag checks).
- Puts process execution in the trust layer that owns the token.

### Probe tools found on `PATH`

Pros:

- Finds tools without configuration.

Cons:

- Reports binaries the engine never runs, contradicting ADR-0010's absolute-binary rule; a misleading
  "available" is worse than an honest "not configured".

Rejected for execution. Accepted in a narrower form (decision 4a): a stat-only lookup that never runs the
file and reports only "found" as `detected_no_adapter` (or `found_on_path` for an unconfigured Codex),
never "available".

### Run `--version` on the agent CLIs found on `PATH`

Pros:

- Could show their versions.

Cons:

- Executes third-party binaries the engine has no adapter for and will not run; their startup can read
  configuration, update themselves or contact the network. A found/missing fact is enough to explain why
  they cannot be used yet.

### Emit detection as events or persist the report

Pros:

- Auditable history of what was detected.

Cons:

- Detection is not an execution transition; persisting machine facts adds data at rest with no consumer.

## Consequences

### Positive

- The welcome screen and template notices show facts the engine itself uses, through one exact route.
- Failures are rows with reason codes, never a failed sidecar or a guessed state.

### Negative / trade-offs

- A third-party binary's behaviour under `--version`/`--help` cannot be guaranteed network-free; the probes
  are the minimal ones the engine already runs before an agent node and carry no credentials or `HOME`.
- "Codex available" does not mean agent nodes run from the desktop; the UI must say so until `serve` runs
  agents.

## Compatibility and migration

Additive: two new routes and an optional `serve` flag. Graph schema, events, run store, existing routes and
`agentplane run` are unchanged. The renderer reaches the routes only after the Electron allowlist adds
exactly `GET /capabilities` and `POST /capabilities/check` (F2.11, pending desktop slice).

## Validation

Go tests with fake binaries cover each Git outcome, exact argv and environment, timeout termination, an
unsupported platform starting no process, adapter mapping including panics, report sanitisation with
marker strings, single flight under concurrency (race detector), no store writes, auth on both routes, body
rules, 503 without a detector, the `serve` flag and the composed report. The agent-CLI rows are covered by
an injected lookup (found, missing, Codex found but not configured, Windows) and by a real `exec.LookPath`
over marker-writing scripts that asserts no marker appears.

## Amendment: Git is resolved from PATH

Status: accepted. The engine used to run Git only at the fixed path `/usr/bin/git`, which blocked Git
evidence and workspace isolation on Windows and on systems with Git elsewhere. `gitlocate.ResolveGit` now
uses `exec.LookPath("git")`. The trust boundary moves from a fixed path to the user's `PATH`:

- A relative or current-directory result (`exec.ErrDot`, or any non-absolute path) is refused, so a
  repository cannot plant a `git` that the engine then runs. The error says so.
- The child process still gets a minimal fixed environment, `core.hooksPath=/dev/null` and
  `core.fsmonitor=false`; `PATH` is never inherited by Git.
- One guard (package `internal/gitlocate`) serves `gitstate`, `internal/runtime` and capability detection: on Darwin, a result equal to
  the `/usr/bin/git` shim is executed only after a stat-only check finds developer tools
  (`gitlocate.Locator.Check`). A Git elsewhere on `PATH` (for example Homebrew) is not guarded.
- Detection reports the Git the engine would run. The row stays `unsupported_platform` outside Linux and
  Darwin (unchanged by this amendment).
- The Windows environment adds `SystemRoot`, `TEMP`, `TMP` and Git's own directory on `PATH`. `/dev/null`
  config paths rely on Git for Windows mapping them to NUL; not verified on a Windows runner.

## Amendment: shell and optional Docker rows (issue #188, owner decision Q17, 2026-11-26)

Status: accepted (owner decision Q17 recorded 2026-11-26; text accepted by the owner 2026-11-27), additive within version 1. `tools` gains two trailing `tool` rows after the agent CLIs:
`shell` (a stat of `/bin/sh` on Linux and macOS, `unsupported_platform` elsewhere) and `docker` (the same
stat-only `PATH` lookup as decision 4a; found is `available` with no version, otherwise `missing`). Neither is
ever executed, no daemon is contacted and no path is reported. Docker is informational and optional; nothing
runs in it (target F3.12). No new status or reason code is introduced, so clients that know version 1 already
render the rows. See F2.11 requirements 4b and 4c.
