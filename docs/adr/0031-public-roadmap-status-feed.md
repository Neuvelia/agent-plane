# ADR-0031 - Public roadmap status feed and landing synchronization

**Status:** proposed; decision 2 amended by ADR-0038 (status comes from `docs/roadmap/features.yaml`)
**Date:** 2026-11-16

## Context

The landing page duplicates roadmap facts by hand and is stale (see
`docs/features/REL2-public-roadmap-status.md`). The landing repository is a static
Vite site with a build-time marker system (`<!-- @changelog ... -->`,
`<!-- @include ... -->`) and Cloudflare static-asset deploys. It has no server and
no runtime dependencies, and its CI runs `npm run check`. Status facts live in this
repository's specs and `docs/roadmap.md`. A public statement of progress is a
trust-sensitive output, and a cross-repository credential is a security surface.

## Decision

1. **One generated, versioned file** (`docs/roadmap.json`, `schema_version` 1) is the
   only interface between the repositories. It contains allow-listed fields and no
   dates or timestamps.
2. **Status stays in the specs.** The existing `**Status:**` line gets a closed
   vocabulary; phase state is derived by rule, never typed by hand.
3. **Pull with an optional nudge.** The landing repository runs a scheduled workflow
   (daily) and on `workflow_dispatch` that reads the file from this repository's
   default branch and, when it differs, opens a pull request. This repository may
   additionally send a `repository_dispatch` on merge so updates arrive in minutes.
   If this repository is private, the pull needs a read-only credential; if public,
   it needs none.
4. **Human-reviewed.** The landing change is always a pull request, never a direct
   push. CI validates the file; a person merges; Cloudflare deploys.
5. **Build-time rendering.** The landing renders ledger, rail and a `/roadmap/` page
   from the file with new `<!-- @roadmap ... -->` markers, so the page remains
   complete without JavaScript.
6. **Wider sharing** reuses the same file: the `/roadmap/` page, a stable raw JSON URL
   for others to consume, and links from release notes. Dates and voting are not
   part of this decision.

## Alternatives considered

### Landing fetches the file at build time

Pros:

- No sync pull request; always current at deploy time.

Cons:

- A deploy needs a trigger anyway (a Cloudflare deploy hook is another secret), a
  network failure breaks the build, and nobody reviews what goes public. Rejected.

### Runtime fetch in the visitor's browser

Pros:

- Instant updates, no rebuild.

Cons:

- Breaks the no-JavaScript guarantee, adds a third-party request from a privacy-minded
  product's page, and needs CORS and caching rules. Rejected.

### GitHub Projects or issues as the public roadmap

Pros:

- Built-in voting and comments.

Cons:

- Status would live outside the specs and drift again; limited control of public
  wording. Can link from the page later.

### Git submodule or monorepo

Pros:

- No sync mechanism.

Cons:

- Couples two release cadences and pulls product source into the marketing build.
  Rejected.

## Consequences

### Positive

- One source of truth; no hand-edited counts. Public claims follow merged specs.
- Reviewable publication; the failure mode is a stale page, not a wrong one.
- Contract is tiny and versioned; other consumers (README badge, docs) can use it.

### Negative / trade-offs

- Specs must keep a clean status vocabulary; the generator will force existing
  inconsistencies to be fixed first.
- Up to a day of delay if the nudge fails.
- A cross-repository credential must exist (read-only for a private source,
  pull-request scoped on the landing side).

## Compatibility and migration

No product behavior changes. Existing spec status text is normalized to the
vocabulary in the same change that introduces the generator. The landing hand-written
section is replaced by markers; until then it must be corrected manually (it is
currently stale and its Windows claim does not match the engine: Agent and Gate
nodes fail closed on Windows).

## Validation

REL2 acceptance criteria: deterministic generator, staleness check in `make check`,
a landing pull request within ten minutes of a status change, rendering tests, and a
scoped credential.
