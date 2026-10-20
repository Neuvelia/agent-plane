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
