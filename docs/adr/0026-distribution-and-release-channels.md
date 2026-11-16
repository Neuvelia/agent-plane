# ADR-0026 - Distribution channels, desktop shell and release tooling

**Status:** proposed (shell, license, repository and bundle id decisions accepted by the owner)
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
2. **Shell.** Electron is the desktop shell, for the alpha and beyond. Tauri is
   rejected (owner decision, 2026-11-16); the architecture plan's Tauri
   recommendation is superseded for this product. The renderer and engine stay
   independent of the shell through the loopback API, but no migration is planned.
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
6. **License.** PolyForm Noncommercial 1.0.0 with a commercial license and a CLA
   (owner decision). Rationale: the landing terms already sell per-person work
   licenses; a published Apache-2.0 release could never be tightened afterwards,
   while a restrictive license can be relaxed later. The project is called
   "source-available", not "open source".
7. **Release safety.** Signing and notarization are a follow-up (release secrets in
   a tag-only protected environment, all platforms green before publishing), after
   which an opt-in updater can be reconsidered.
8. **Platforms.** Linux and macOS only until Windows process-tree termination for
   Agent and Gate nodes is specified and tested.

## Alternatives considered

### Apache-2.0 (rejected)

Pros:

- Recognized open-source license; widest adoption and contribution.

Cons:

- Contradicts the per-person work license on the landing; irrevocable for every
  published version.

### Tauri (rejected)

Pros:

- Smaller footprint; the architecture plan recommended it.
- Rust core fits a signed, sandboxed shell.

Cons:

- Rewrites the Electron main process, the preload and tests, and adds a Rust
  toolchain to CI, with no user-visible gain.
- Delays the first public build.
- Rejected by the owner: one shell, one toolchain.

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

- Electron's footprint ships in the product; the team owns Electron security
  updates (version bumps, `contextIsolation`, CSP) as a standing maintenance task.
- Unsigned artifacts cause OS warnings; users must follow documented steps.
- Linux and macOS only; Windows users wait.
- Users update by hand.

## Compatibility and migration

No graph, event, store or API change. The CLI `version` output gains fields; its
first line remains the bare version. The loopback API stays the boundary between shell and engine (ADR-0019, ADR-0021).

## Validation

REL1 acceptance criteria: reproducible CLI builds, per-OS smoke tests of the
extracted archive and the packaged app, no outbound requests from the app, and the
sidecar override test.
