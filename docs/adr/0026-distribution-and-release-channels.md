# ADR-0026 - Distribution channels, desktop shell and release tooling

**Status:** proposed
**Date:** 2026-11-16

Numbering note: ADR-0024 and ADR-0025 are used by branches that are not yet
merged (workspace isolation and attempt diff capture / capability report). This
ADR takes 0026; renumber at merge if those change.

## Context

The product has no distribution path (see `docs/features/REL1-release-engineering.md`).
Four decisions are expensive to reverse once users install the app: where
artifacts are published, which desktop shell is shipped, which tool builds the
artifacts, and whether the app updates itself. The architecture plan recommends
Tauri for the final product and Go for the engine; the repository implemented the
desktop shell in Electron (ADR-0019). The engine has no cgo, so cross-compiling
is cheap. The architecture rules require that nothing sends data or contacts a
remote service implicitly.

## Decision

1. **Channel.** GitHub Releases are the single channel for alpha: CLI archives,
   desktop packages, `SHA256SUMS` and provenance attestations. Package managers
   come later and only point at these artifacts.
2. **Shell.** Ship the existing Electron shell for the alpha. Revisit Tauri after
   Phase 3, when the engine API and the sidecar contract are stable. The renderer
   (React, talking to the loopback API) and the engine do not depend on Electron;
   only `desktop/electron/` (main, preload, allowlist, recents, graph dialogs) would
   be rewritten.
3. **Tooling.** The CLI is built by a small script in `scripts/` driven by a GitHub
   Actions matrix (`CGO_ENABLED=0`, `-trimpath`, `-ldflags -X` for version, commit
   and date). The desktop is packaged with `electron-builder`, a development-only
   dependency pinned in `desktop/package.json`, used only by the release workflow.
4. **Updates.** No auto-update in alpha. The app makes no network request on its
   own. Revisit with signing: an updater needs signed artifacts and an explicit
   user opt-in.
5. **Sidecar trust.** A packaged app runs only the sidecar from its own resources and
   only when versions match. Environment overrides apply to unpackaged development
   runs only.
6. **Platforms.** Linux and macOS only until Windows process-tree termination for
   Agent and Gate nodes is specified and tested.

## Alternatives considered

### Tauri now

Pros:

- Smaller footprint; the architecture plan recommends it.
- Rust core fits a signed, sandboxed shell.

Cons:

- Rewrites the Electron main process, the preload and tests, and adds a Rust
  toolchain to CI, before the contracts are stable and with no user demand yet.
- Delays the first public build for little user-visible gain.

### GoReleaser for the CLI

Pros:

- Archives, checksums, changelog and Homebrew formulae out of the box.

Cons:

- A new tool and configuration language for four targets that a short script and a
  matrix cover. The dependency policy prefers the standard library and plain
  scripts unless a tool materially reduces risk.
- Can be adopted later without changing artifacts; archive names and checksums are
  the contract, not the tool.

### Electron Forge instead of electron-builder

Pros:

- Maintained by the Electron project.

Cons:

- Less direct control of `extraResources` and per-OS targets for the sidecar; both
  are workable. Choose at implementation time if spike results differ; the
  contract (package layout, sidecar from resources) does not change.

### Auto-update from day one

Pros:

- Users stay current.

Cons:

- Implicit network traffic, needs signing first, and conflicts with the
  local-first default. Rejected for alpha.

## Consequences

### Positive

- First public build needs no new language or service and no secrets.
- The sidecar rule closes an environment-variable engine swap in installed apps.
- Artifact names and checksums stay stable if the tooling changes.

### Negative / trade-offs

- Electron's footprint ships in the alpha.
- Unsigned artifacts cause OS warnings; users must follow documented steps.
- Linux and macOS only; Windows users wait.
- Users update by hand.

## Compatibility and migration

No graph, event, store or API change. The CLI `version` output gains fields; its
first line remains the bare version. Moving to Tauri later replaces only the
shell, with the loopback API as the stable boundary (ADR-0019, ADR-0021).

## Validation

REL1 acceptance criteria: reproducible CLI builds, per-OS smoke tests of the
extracted archive and the packaged app, no outbound requests from the app, and the
sidecar override test.
