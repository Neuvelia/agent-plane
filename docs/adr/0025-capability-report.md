# ADR-0025 - Read-only capability report on the loopback API

**Status:** proposed
**Date:** 2026-11-16

## Context

F2.11 shows a new user what their machine can run: the engine version, the Git binary the engine uses and
each coding-agent adapter the engine has. Today these facts appear only as a failed run (`codex_detection`,
`codex_git`, "loop adapter is unavailable"). The renderer is sandboxed and must not run processes or see
paths (ADR-0019, ADR-0021), so the facts must come from the Go sidecar. Adding a route is a durable change to
the loopback API contract, and detection starts processes, which AGENTS.md treats as untrusted-input
territory.

Verified facts that bound the decision:

- The engine runs Git only at `/usr/bin/git` (`internal/gitstate`, `internal/runtime/workspace.go`).
- Codex detection already exists as `codex.Adapter.Detect` (ADR-0009, ADR-0010): an absolute binary,
  `--version`, `--help` and `exec --help` probes with an empty environment, 10 seconds each, Linux and macOS
  only.
- `serve` has no agent adapter; agent nodes cannot run from the desktop. No Claude Code adapter exists.

## Decision

1. **Routes.** The loopback API adds `GET /capabilities` (the last report, detecting first if none exists in
   this process) and `POST /capabilities/check` (empty body or `{}`; detects again). Both sit behind the
   existing bearer-token and `Host` checks, write no CORS headers, bound the body to 64 KiB and are
   registered by exact method and path. Without a configured detector they answer 503.
2. **DTO, version 1.** `{version, checked_at, engine: {version, os, arch}, tools: [{id, kind, status,
   version, reason}]}`. `tools` is `git` followed by the composed adapters sorted by id (today exactly
   `codex`). `status` is `available`, `missing`, `not_configured`, `unsupported_platform`,
   `unsupported_version` or `error`; `reason` is a closed set of codes. `version` is set only for
   `available` and must match `^[0-9A-Za-z.+-]{1,64}$`. The report never contains a path, an environment
   value, error text or raw probe output.
3. **Detection is the engine's own.** Git: on Linux/Darwin only, stat `/usr/bin/git`, then one bounded
   `--version` probe with structured argv, the environment `GIT_CONFIG_NOSYSTEM=1`,
   `GIT_CONFIG_GLOBAL=/dev/null`, `LC_ALL=C` and nothing else, stdin closed, the temp directory as working
   directory, output capped at 4 KiB, a 10 second timeout with termination. Adapters: the existing
   `Detect`, with the error kind (never its text) selecting the status. On other operating systems no probe
   process starts and every row is `unsupported_platform`. There is no `PATH` lookup. On Darwin,
   `/usr/bin/git` is an `xcrun` shim that opens the "install developer tools" dialog when no developer
   tools exist, so it is executed only after a stat (no execution) finds a real Git under the Command Line
   Tools, the `xcode_select_link` target or the default Xcode location; otherwise the row is `missing`
   with `developer_tools_missing`. This is unit-tested with injected stat/readlink and still needs a
   native macOS check.
4. **Codex binary.** `serve` accepts an optional `--codex-bin ABSOLUTE_PATH` used only to compose detection;
   agent execution in `serve` is unchanged. Without it Codex is `not_configured` and `Detect` is not called.
   Electron main passes it only from an absolute `AGENTPLANE_CODEX_BIN`.
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
rules, 503 without a detector, the `serve` flag and the composed report.
