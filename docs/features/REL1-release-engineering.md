# REL1 - Release engineering (alpha binaries and desktop installers)

**Status:** draft
**Priority:** P0 for the first public preview (not a roadmap F-ID; see "Roadmap placement")
**Target phase:** between v0.3 (Local IDE) and v0.4 (Local Production Ready)

## Roadmap placement

The product roadmap (F0-F6) has no feature for packaging, signing, versioning or
publishing. F2.11 (onboarding, capability detection) is the nearest and only
covers first run, not distribution. REL1 is an engineering enabler, not a product
phase. It adds no execution behavior and must not change graph, event or run
semantics.

## Problem

AgentPlane builds only from source. `LICENSE` exists (PolyForm Noncommercial 1.0.0); there is no release tag, the
version is a constant (`0.0.0-dev` in `internal/version`), nothing builds
archives, and the desktop app cannot run outside a checkout: `desktop/electron/main.cjs`
looks for `bin/agentplane` next to the repository (or in `AGENTPLANE_BIN`), so a
packaged app would not find its engine. Nobody outside the repository can install it.

## Goal

A maintainer pushes a version tag and gets, without manual steps, a draft GitHub
Release with verifiable CLI archives and unsigned desktop installers for the
platforms that the engine really supports, plus a documented install path and an
honest statement of limits.

## Non-goals

- Code signing, Apple notarization, Windows signing certificates (a later step;
  alpha artifacts are explicitly unsigned).
- Auto-update. An updater is network egress that the user did not ask for.
- Package managers (Homebrew tap, winget, apt/rpm repositories), Docker image.
- Any other desktop shell. Electron is the decided shell (ADR-0030).
- Bundling Git or the Codex CLI. They stay external prerequisites.
- Any cloud service, account, telemetry or crash reporting.
- Windows artifacts, until the Windows gaps below are closed.

## Prerequisite: rename `agentplane` to `tarmind` (decided)

Public artifacts use the product name Tarmind. The CLI binary, desktop app and
bundle identifier must be named before the first tag, because a bundle id and an
installed command are expensive to change once users have them. The rename is a
separate change that lands **before** REL1's release workflow, and it must not mix
with behavior changes.

Measured footprint at the time of writing: 171 files mention `agentplane`, 87 Go
files import `agentplane/...`, `cmd/agentplane/` is the entrypoint, and these
environment variables exist: `AGENTPLANE_BIN`, `AGENTPLANE_STORE_DIR`,
`AGENTPLANE_ATTEMPT` (injected into agent and shell processes, so it is a contract
that graphs and scripts may read), plus test-only `AGENTPLANE_*` helpers.

Required rename scope:

- CLI binary and command name `tarmind`; `cmd/agentplane` becomes `cmd/tarmind`;
  Go module path; usage text and all error messages that name the command.
- Environment variables `AGENTPLANE_*` become `TARMIND_*`. Because `AGENTPLANE_ATTEMPT`
  is visible to user scripts, the engine sets both names for one release and
  documents the old one as deprecated; reading `AGENTPLANE_BIN` and
  `AGENTPLANE_STORE_DIR` is dropped (no public release exists yet to break).
- Desktop: package name, window title, bundle identifier, Electron `userData`
  directory name. The old directory is not migrated automatically (pre-release);
  the runbook states where old local runs live.
- Docs, ADR text, README, examples, `.github` workflows, Makefile targets.
  Historical ADRs keep their original wording except for a one-line note that the
  product was renamed; do not rewrite history.
- The `.agents/` directory and `agentplane`-prefixed JSON or event field names:
  **no change** unless a separate decision says so. Event and graph schemas are
  durable contracts; the rename must not touch `schema_version` semantics.

Sequencing: the rename touches almost every file, so it conflicts with every open
branch (including the F3.1 stack and the clean-architecture refactor stack). Do it
after those merge or rebase them onto it in one agreed step, as one mechanical PR
(type-aware rewrite plus a verifying grep), reviewed separately from REL1.

Acceptance for the rename: `grep -ri agentplane` returns only the allowed leftovers
(historical ADR notes, the deprecated `AGENTPLANE_ATTEMPT` alias, the rename note);
`make check` and `make desktop-e2e` pass unchanged; golden CLI tests differ only in
the command name.

## Supported platforms for the first release

Linux (amd64, arm64) and macOS (arm64, amd64). On Windows the Codex adapter
(`internal/agent/codex/adapter.go`) and the Gate command (`internal/gate/command.go`)
fail closed by design, so Agent and Gate nodes do not run there. A Windows
release is allowed only after a feature spec defines Windows process-tree
termination and the CI Windows job is green. Until then no Windows binary or
installer is published, and public copy must not claim Windows support.

## Functional requirements

1. **License.** The repository is source-available under the PolyForm Noncommercial
   License 1.0.0 (`LICENSE`, with `Required Notice:` lines naming NEUVELIA sp. z o.o.
   and the commercial-license contact, plus `NOTICE`); owner decision, 2026-11-16.
   It is not an open-source license, and no public text may call Tarmind "open
   source" (use "source-available"). Commercial use needs a work license, as the
   landing terms and pricing already state. Release archives and installers include
   `LICENSE`, `NOTICE` and a generated `THIRD_PARTY_NOTICES`; the packaged desktop
   app also includes Electron's `LICENSE`, `LICENSES.chromium.html` and the Geist
   font licenses. A CI check fails when a dependency has a license that cannot be
   redistributed with this one. Because a published version cannot be re-licensed,
   no artifact and no public repository may exist before this requirement is met.
2. **Version stamping.** `tarmind version` prints the semantic version from the
   release tag, the commit and the build date. Builds without stamping print
   `0.0.0-dev` as today. The value is injected at build time (`-ldflags -X`), not
   read from the network or the environment at run time.
3. **CLI archives.** For each supported target a release job builds a static binary
   (`CGO_ENABLED=0`, `-trimpath`) and packs `tarmind`, `LICENSE` and a short
   `README` into a `.tar.gz`. A `SHA256SUMS` file covers every artifact.
4. **Draft release.** A tag matching `v*` creates a draft GitHub Release with all
   artifacts and generated notes. Publishing stays a manual maintainer action.
   Pre-release tags (`-alpha.N`, `-rc.N`) are marked as pre-releases.
5. **Desktop packaging.** The desktop app is packaged per target (macOS `.dmg`,
   Linux `AppImage` and `.deb`). The Go sidecar for the same target is copied into
   the package resources.
6. **Sidecar resolution.** In a packaged app the main process resolves the sidecar
   only from `process.resourcesPath`. The `AGENTPLANE_BIN` override is honored only
   when the app is not packaged, so an environment variable cannot swap the engine
   of an installed app.
7. **Sidecar integrity.** The packaged app refuses to start a sidecar whose version
   differs from the app's own version, with an actionable error.
8. **Update behavior.** The app makes no network request on its own. A manual
   "check for updates" link may open the Releases page in the default browser.
9. **Supply chain.** Third-party actions are pinned by commit SHA. Workflow
   `permissions` default to read-only. Only the release job has `contents: write`.
   Release builds use `npm ci --ignore-scripts` for installs and fail on a changed
   lockfile. Artifacts get GitHub build-provenance attestations.
10. **Documentation.** `README.md` has an Install section (download, verify
    checksum, first run, macOS Gatekeeper and Linux AppImage steps for unsigned
    builds, prerequisites). `docs/release.md` is a maintainer runbook (cut a tag,
    what the workflow does, how to roll back by deleting a draft).
11. **Truthful limits.** The release notes template and the install docs state the
    current limits: shared workspace without isolation, no resume after a restart,
    unsigned builds, supported platforms.

12. **Contributions.** Outside contributions are accepted only after the author
    signs a Contributor License Agreement that lets the owner license the
    contribution commercially (a DCO does not). A workflow checks every pull
    request and never checks out or runs the pull request's code. Until the CLA
    exists, outside pull requests are not merged.
13. **Release environment.** Release secrets (signing keys, notarization and
    signing-service credentials, once signing is added) live only in a protected
    CI environment that admits tags only. Every platform job must pass before the
    release is published; no partially published release.
14. **Provider terms.** A document (`docs/provider-terms.md`) states how Tarmind
    starts third-party agent CLIs, that it does not bundle or resell them, that the
    user's own account and terms apply, and what each provider publishes about
    automated use. Vendor CLIs are never included in the installer.
15. **Electron hardening.** The packaged app turns off `runAsNode` and the
    `NODE_OPTIONS` and inspector switches, loads only from its ASAR with integrity
    validation, and keeps the existing context-isolation, sandbox and CSP rules.
    A test reads the package's fuse configuration.

## Data / API contracts affected

- CLI: `tarmind version` output gains commit and build date (output format
  documented in the runbook; the first line stays the bare version for scripts).
- Desktop: sidecar lookup rules in the Electron main process (ADR-0021 trust
  boundary unchanged: the renderer still never sees the path).
- No graph schema, event schema, run store or HTTP API change.

## Failure behavior

- A tag whose version does not parse as semver fails the release job before any
  artifact is built.
- A failed build, test or checksum step on any target fails the whole release and
  leaves no draft.
- A missing or version-mismatched sidecar in the packaged app shows an error
  screen naming the expected and found versions; it does not fall back to a
  `PATH` lookup.
- A missing prerequisite (Git, Codex CLI) is reported by the existing capability
  report, not by the installer.

## Acceptance criteria

- [ ] `LICENSE` (PolyForm Noncommercial 1.0.0, official text plus `Required Notice:`
      lines), `NOTICE` and `THIRD_PARTY_NOTICES` exist and are present in every CLI
      archive and desktop package; the desktop package also contains Electron's
      `LICENSE` and `LICENSES.chromium.html` (test lists the package contents).
- [ ] A dependency with a non-redistributable license fails the license check (test
      with a fixture); a text search of README, docs and landing copy finds no claim
      that Tarmind is "open source".
- [ ] Building the same tag twice produces byte-identical CLI binaries (compare
      SHA-256 in CI).
- [ ] `tarmind version` from a release archive prints the tag's version; the
      same from a plain `go build` prints `0.0.0-dev`.
- [ ] A test tag on a branch produces a draft release with one archive per
      supported target and a `SHA256SUMS` that `sha256sum -c` accepts.
- [ ] On each supported OS, CI extracts the archive and runs `graph validate` and
      `run` on a three-node Shell example graph; exit codes match the CLI tests.
- [ ] On each supported OS, CI installs or unpacks the desktop package, starts it
      headlessly where possible, and asserts: the sidecar listens on loopback only,
      the app reaches a running state, and quitting leaves no sidecar process.
- [ ] With `AGENTPLANE_BIN` set to a different binary, a packaged app still starts
      the bundled sidecar (test).
- [ ] A packaged app whose sidecar reports a different version shows the mismatch
      error and does not start runs (test).
- [ ] Observed with a packet capture or proxy log during the smoke test, the app
      makes no outbound request except to the loopback sidecar.
- [ ] Workflow lint passes: all third-party actions pinned by SHA; only the
      release job requests `contents: write`.
- [ ] No Windows artifact is published and the README and release notes list
      supported platforms exactly as this spec does.

## Required tests

- Unit: version stamping and the `version` output format; semver tag parsing in the
  release script; sidecar path resolution for packaged and unpackaged cases
  (`desktop/test`).
- Integration: archive layout and checksum verification; reproducibility check.
- CLI/e2e: per-OS smoke of the extracted archive; per-OS packaged desktop smoke
  (extends `desktop/e2e/demo.cjs` to run against the package).

## Security / trust considerations

- Untrusted inputs: tag names, environment variables, release asset names, and the
  contents of any downloaded installer. Tag names are validated before use in
  shell commands; workflow expressions never interpolate untrusted text into `run:`.
- Unsigned artifacts are a real risk. Mitigations in alpha: checksums, provenance
  attestations, and instructions that never tell users to disable OS protections
  globally.
- The release job holds no secrets in alpha. When signing is added, keys must live
  in the CI secret store, never in the repository, and never in logs.
- A packaged app must not widen filesystem or process permissions compared with
  `make desktop-e2e` today.

## Observability

No runtime events change. The release workflow logs each artifact name, size and
SHA-256, and the attestation reference, as its audit trail.

## Open questions

1. Resolved: PolyForm Noncommercial 1.0.0 (see requirement 1). Open: the exact
   commercial-license contact address (`hello@neuvelia.com` is used from the landing
   terms); who prepares the CLA text (a lawyer should; do not copy another project's
   agreement); click-through bot or signed PDF; whether PolyForm Small Business is
   preferable (free for small companies) once pricing is final.
2. Resolved: the public repository is this one, renamed from `agent-plane` to
   `tarmind`. The landing repository stays private. Before the visibility change:
   review the history and docs (audits, plans, prompts, thesis experiment data) and
   run a secret scan.
3. Resolved: bundle identifier `run.tarmind.desktop` (domain `tarmind.run`); app
   name Tarmind; binary `tarmind`.
4. Resolved: Electron (ADR-0030).
5. Should arm64 Linux be in the first release, or only macOS arm64 and Linux amd64?

## Out of scope for this iteration

- Signing and notarization; Windows; auto-update; package managers; SBOM; an
  installer for Git/Codex; delta updates; release channels beyond pre-release vs
  release.
