# REL2 - Public roadmap status feed

**Status:** draft
**Priority:** P1 (engineering enabler; depends on the public/private decision below)
**Target phase:** alongside REL1

## Problem

The landing page (`tarmind-landing`, section `#status`) shows a hand-written
ledger and roadmap rail. It is already stale: it says "14 of 15 done", Phase 1
"now" and `F1.8` "next", while the repository has implemented F1.8 and a large
part of Phase 2. Two hand-maintained copies of the same facts will always drift,
and a public page that claims "done only when acceptance criteria are covered"
must not overstate progress.

## Goal

The roadmap and feature status shown publicly are generated from the repository's
own specs, reviewed before they go live, and updated by a pull request in the
landing repository, with no manual copying.

## Non-goals

- Publishing PR titles, branch names, issue text, internal plans or dates.
  The public view shows Now / Next / Later, never delivery dates.
- A live API or a runtime fetch from the visitor's browser.
- Voting, comments or an issue-tracker UI (see "Wider sharing" in ADR-0031).
- Changing how specs are written beyond one machine-readable status line.

## Source of truth

> **Amended by ADR-0038 (2026-11-26):** phase, ID, title, priority and status come from
> `docs/roadmap/features.yaml`; the generator below must read the registry instead of
> `docs/roadmap.md` and the spec status lines. The rest of this section is the original
> draft and must be rewritten before implementation.

`docs/roadmap.md` supplies phase, ID, title and priority. Each spec under
`docs/features/` supplies status through its existing `**Status:**` line, using a
closed vocabulary: `draft`, `accepted`, `in progress`, `implemented`. A roadmap ID
without a spec is `planned`. A spec may add an optional `**Public note:**` line
(one sentence, for example "real-agent demo not yet run") that is published as a
known limit. Nothing else from the specs is exported.

## Functional requirements

1. A deterministic generator (Go, standard library, under `scripts/`) reads the
   roadmap tables and spec headers and writes `docs/roadmap.json`. Same input
   gives byte-identical output; no timestamps in the file.
2. `docs/roadmap.json` has an explicit `schema_version`. Each item has `id`,
   `title`, `phase`, `priority`, `status`, optional `public_note`, and `spec_path`.
   Each phase has `id`, `name`, `goal`, `release` (label only) and a derived state
   `done | now | next | later`.
3. Phase state is derived, not typed: `done` when every P0 item is `implemented`;
   `now` for the first phase that is not done and has any item beyond `planned`;
   the following phase is `next`; the rest are `later`.
4. The generator rejects: an unknown status value, a duplicate ID, a spec whose ID
   is missing from the roadmap, and a roadmap ID whose spec file name does not match.
   Errors name the file and line.
5. `make roadmap` regenerates the file; `make check` fails if the committed file is
   stale (same pattern as the desktop token sync check).
6. A workflow in this repository, on push to `main` that changes the roadmap or any
   spec header, notifies the landing repository (mechanism in ADR-0031). The landing
   repository opens a pull request that updates its copy of the file; a human
   merges it. No direct push to the landing `main`.
7. The landing build renders the ledger, the rail and (new) a `/roadmap/` page from
   that file at build time, using the same marker pattern as the changelog
   (`<!-- @roadmap ... -->`). The page works without JavaScript. Counts, progress
   bar, "done / next" pills and the "Known limit" line come from the data.
8. The landing build fails when the file is missing, has an unknown
   `schema_version`, or contradicts itself (for example a `done` phase containing a
   non-implemented P0 item).

## Data / API contracts affected

- New public contract: `docs/roadmap.json` (versioned schema).
- Spec header convention: closed `Status` vocabulary and optional `Public note`.
- No runtime, graph, event or CLI change.

## Failure behavior

- Stale or invalid feed: landing keeps its last good copy (the sync PR is not
  merged; CI on the landing repository fails the PR with the validation error).
- Notification failure: the landing repository also syncs on a daily schedule and on
  manual dispatch, so a missed notification delays updates by at most a day.
- Status disagreement (a spec says `implemented` but a PR is open): the feed follows
  the merged spec on `main`; open PRs never change public status.

## Acceptance criteria

- [ ] Running the generator twice yields identical bytes; it contains no time or path
      of the local machine.
- [ ] Each rejection case in requirement 4 has a test with the exact error text.
- [ ] `make check` fails when a spec status changes without regenerating the file.
- [ ] Given the current `main`, the generator output matches an independently
      written expectation for phase states (Phase 0 and 1 done or with a named
      limit, Phase 2 now, Phase 3 next) reviewed by a human once.
- [ ] A status change merged to `main` produces a landing pull request containing
      only the data file change, within 10 minutes of the merge.
- [ ] The landing build renders the ledger count and progress bar from the data;
      changing one item from `implemented` to `in progress` in a test fixture
      changes the count, the pill, the bar value and `aria-valuenow`.
- [ ] The rendered `/roadmap/` page has no script requirement and passes the
      landing repository's existing contrast and lint checks.
- [ ] The cross-repository credential is scoped to one repository and one permission
      set (pull requests and contents on the landing repository only), and is
      absent from logs.

## Required tests

- Unit: generator parsing, phase-state derivation, rejection cases.
- Integration: golden file for the repository's real roadmap and specs.
- Landing: renderer tests with fixtures (as `vite/changelog.test.ts`), build failure
  on invalid data.

## Security / trust considerations

The feed is public output derived from untrusted-ish text (spec headers). The
generator exports only allow-listed fields and escapes them; the landing renderer
treats every string as text, not HTML. Secrets, internal URLs, branch names and
PR content must never enter the file. The sync credential is a fine-grained token
or GitHub App installation limited to the landing repository.

## Observability

The sync workflow logs the feed hash and item counts per status. The landing PR
body lists changed items (ID, old status, new status).

## Open questions

1. Resolved: the public source repository is `Neuvelia/tarmind` (this repository,
   renamed); the landing repository is private. The landing pulls the public file
   without a read credential and opens its sync PR with its own workflow token, so
   the cross-repository secret is not needed for the pull model.
2. Resolved: pull on a schedule and on manual dispatch (ADR-0031); an optional
   push-style nudge needs a token and is deferred.
3. Which spec status shows as "Known limit" on the landing: only `Public note` lines,
   or also a fixed platform statement (Windows)?

## Out of scope for this iteration

Dates and release targets on the public roadmap; per-feature progress percentages;
usage statistics; an RSS feed of status changes (can reuse the landing changelog).
