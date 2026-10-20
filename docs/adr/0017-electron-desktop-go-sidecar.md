# ADR-0017 — Electron desktop with a private Go sidecar

**Status:** accepted; implementation in progress
**Date:** 2026-10-20

## Context

Phase 2 requires an installed desktop application for macOS/Linux/Windows.
Execution already lives in Go. The renderer handles untrusted repository text
and process output; giving it process/filesystem privileges widens that boundary.

## Decision

Use Electron main + context-isolated typed preload + local React renderer,
with Electron main owning one bundled Go sidecar through private stdio. Go owns
validation, execution and history. No local HTTP listener is needed. Keep a
separately versioned service protocol with hello/version/capability negotiation,
request IDs, named typed methods, structured errors and 8 MiB frame limits.
Page larger responses; never concatenate graph commands into RPC shell strings.

Each request has protocol_version, request_id, method and typed params; each
response echoes request_id with exactly one result/error. Errors carry code,
safe message, retryable flag and optional graph/node/field identity. Event pages
carry run_id, after_sequence, watermark, retained facts and omission metadata.
Subscribers attach to a captured watermark before replay/tailing; deduplicate
sequence identities and reconcile final snapshots. Transport never renumbers
runtime events or claims omitted progress was retained.

The service owns a revisioned document buffer and atomic save/conflict detection,
plus preflight tokens bound to exact launch payloads. Start idempotency is keyed
by payload digest and explicit client key. One desktop run is active at a time.
Go must offer retained event snapshot/replay during execution; the bounded live
prefix cannot implement reliable reconstruction alone. Keep collectors and
UI-delivery buffers bounded according to existing progress contracts.

Renderer sandbox on, Node integration off, context isolation on, restrictive CSP,
validated IPC sender, no arbitrary path/process/SQL bridge and no remote renderer.
Native folder selection grants explicit local access. Process ownership must
clean up descendants on all release platforms, including Windows; no fallback
that silently removes Codex safety flags. Exact dependency pins and OS minima
are implementation-plan decisions reviewed before installation.

The Go source-build minimum rises from 1.23 to 1.25. Traversal-resistant
`os.Root` access confines local project files, and pinned
`golang.org/x/sys v0.46.0` supplies named Darwin/Linux exchange calls that the
standard library lacks. It is compiled into the bundled sidecar, so installed
users do not need Go. Offline CI/build hosts must preinstall Go 1.25 or newer
and use `GOTOOLCHAIN=local` to avoid hidden toolchain downloads. The platform
exchange only preserves a displaced file; it does not provide an atomic
digest compare-and-swap. Saves need durable intent and recovery before they
can claim conflict safety.

The local project/workspace/settings registry is a version-1 JSON file in an
absolute private app-user directory. The state directory is mode 0700 and its
regular file is mode 0600, capped at 1 MiB; unknown fields, duplicate keys,
invalid IDs and newer versions fail closed rather than resetting state. A
separate stable advisory lock file is held for the service lifetime so atomic
replacement of the JSON file cannot release cross-process exclusion. Project
and workspace registrations persist canonical paths with pinned filesystem
identities; a missing or replaced location becomes unavailable without
retargeting its ID. Folder grants are short-lived, one-use memory tokens issued
only for Electron main's native folder selection path. Registry writes sync an
adjacent temp file, atomically replace the JSON file and sync the directory. A
post-replacement sync failure blocks further service mutations until restart
because durability is uncertain. The Windows native lock, replacement and
reparse/ACL checks must pass before Windows registry support is enabled.

Graph validation returns the existing Go diagnostic codes with flat typed
locations and fixed code-specific text, rather than echoing authored content
inside messages. A bounded response retains at most 1,000 findings within a
1 MiB diagnostic JSON budget and reports the
total, omitted count and an untruncated `has_errors` flag. Current YAML parsing
does not claim source line or column. Preflight requires a valid saved document
whose exact bytes and file identity still match disk, plus a selected workspace
from the same registered project. It checks typed inputs and static node
configuration without running graph commands. Agent capability is derived from
the configured executable and the runtime's actual Git path; Shell-only graphs
do not need either. Green preflight creates an opaque in-memory token bound to
document/workspace identities, input bytes, settings and detected capability.
Tokens expire after 120 seconds and are bounded to eight entries and 16 MiB of
retained graph text, evicting oldest entries first. Start must recheck these
facts; a token alone does not authorize a stale launch. Recovery of pending
document saves runs before document methods become available; ambiguous
recovery fails the document service closed.

`runs.start` supplies a UUID idempotency key, opaque preflight token, echoed
payload digest, saved document ID/revision, workspace ID, exact typed inputs and
max concurrency. The service binds an accepted key to the logical payload,
excluding the token so a fresh token for identical data can reattach. A first
launch requires a live token and repeats saved-file, workspace, settings,
environment, executable and capability checks under the one-run owner lock.
The configured executable fingerprint includes regular-file identity, size,
modification time and a bounded content hash. Path-based process creation still
has an external replacement race after that check. Shell allowlisted variables
and Codex environment references are captured before Start and passed as frozen
values to the desktop executors; nil frozen maps preserve the CLI behavior.
Environment values enter a service-keyed HMAC capability seal, not a public raw
hash. Explicit environment values and supplied string graph inputs enter the
runtime log redactor within bounded launch limits.

The run owner publishes only the RunID from `runtime.Run`'s readiness callback.
Pre-ready errors release the reservation without a phantom run. Accepted runs
outlive renderer requests while the sidecar remains alive. `runs.cancel` targets
an exact RunID and joins runtime cleanup. The sidecar cancels and joins owned
work on every protocol return, including EOF or pipe error. Cancel responds
with the exact `{run_id,state}` after cleanup, including for an older accepted
run whose trace has been evicted. `runs.snapshot`
returns safe run and node facts plus the retained trace watermark and omission
counts; raw errors, output values and input values stay out of this DTO. Until
F2.9 persists history, the service retains at most 64 accepted key tombstones
and the active plus most recent completed trace. Older trace lookup reports
unavailable rather than fabricating an event stream. Sidecar death does not
resume execution. Existing Shell output capture can still grow during one
active run; this cap bounds cumulative retained desktop runs, not that peak.

Protocol document DTOs carry exact UTF-8 text as canonical standard base64
(`text_base64`, at most 4 MiB decoded). Plain JSON escaping can expand accepted
document text beyond the 8 MiB frame; base64 plus the bounded diagnostics fits
that limit. The preload converts this encoding to renderer-facing text.

## Alternatives considered

A browser UI plus localhost server is possible but conflicts with the requested
installed desktop direction and introduces another exposed transport. Rewriting
runtime in TypeScript duplicates proven contracts. Tauri is an alternative shell,
but Electron is the user's stated preference.

## Consequences

Go and CLI retain ownership of semantics. Desktop adds packaging and bridge
maintenance, and Windows execution portability is required work. SQLite driver
selection remains separate. Developers building from source need Go 1.25 or
newer; existing graph and CLI data formats remain unchanged. No cloud
architecture is introduced.

## Compatibility and migration

Existing graph schema 0 and CLI JSON schema 1 remain unchanged. Layout metadata
is nonsemantic. Human node/event changes belong to ADR-0019. A protocol mismatch
fails safely before launches; do not assume app and arbitrary installed CLI match.

## Validation

Native packaged lifecycle tests on three OSes; malformed IPC/version tests;
renderer privilege tests; descendant cleanup; snapshot/replay overflow and reload;
UI-authored graph validation/execution in CLI. See F2.0 and F2.4.

The Phase 2 specification and visual reference are retained in the local,
ignored `.agentplane/phase2-context/` bundle during implementation. F2.0
establishes the transport; later features register their own typed methods.

## External technical references

- [Electron security guidance](https://www.electronjs.org/docs/latest/tutorial/security):
  isolation, sandboxing, restricted navigation and validated IPC.
- [Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects):
  native process-group ownership candidate for Windows implementation planning.

Checked 2026-10-20; these sources inform the decision, not proof of current portability.
