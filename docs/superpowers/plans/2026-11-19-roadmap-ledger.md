# Roadmap Ledger Adoption Implementation Plan

> Plan only. Do not start before the gate in section 2 is open. Steps use
> checkbox syntax so progress can be tracked in this file.

**Goal:** Make `docs/roadmap-ledger.yaml` the only hand-edited place where a
feature's status is recorded. Every other statement of status is generated from
it or checked against it by `make check`, so a PR that changes status without
updating the ledger fails CI.

**Why:** `docs/audits/2026-11-18-roadmap-tracking-consistency.md` found status
written by hand in about ten places (AGENTS.md, README.md, `docs/roadmap.md`,
`docs/features/README.md`, 40 spec headers, plan checkboxes, ADRs,
`docs/architecture.md`, REL2/landing) and already out of date against the code
(event schema 13 vs 10/11 in docs; F3.2 and F3.4 progress missing).

**Non-goals:** changing execution behavior, events or the graph schema; building
the public landing feed (REL2 stays a consumer); tracking per-task progress of a
plan in the ledger (plans keep their own checkboxes).

## 1. Open pull requests at planning time (2026-11-19)

`main` is still `fdaae01`. Thirteen PRs are open. Their merge will change
statuses the ledger records, which is why adoption waits for them.

| PR | Branch / stack | Touches | Expected ledger effect (verify after merge) |
| --- | --- | --- | --- |
| 144 | `f2-12-more-templates` (main) | F2.12 | template gap closed, `coverage` -> `full` if all four PDF templates land |
| 145 | `f2-3-e5b1-values` (main) | F2.3 | E5b.1 done |
| 148 | `f2-3-e5a2-bindings-panel` (main) | F2.3 | E5a.2 done |
| 152 | `f2-3-e5a3-io-hints` (main) | F2.3 | E5a.3 done; F2.3 stays `in_progress` while E5b.2 is open |
| 151 | `f2-6-run-overview` (base: 145) | F2.6 | gap list shrinks; possibly `implemented` |
| 153 | `f2-7-node-detail` (base: 151) | F2.7 | gap list shrinks; possibly `implemented` |
| 146 | `f2-8b-adapter` (main) | F2.8 | T3 |
| 147 | `f2-8b-runtime` (main) | F2.8 | T4 |
| 149 | `f2-8b-cli` (main) | F2.8 | T5 `--record-diffs` |
| 150 | `f2-8b-docs` (main) | F2.8, ADR-0026 | ADR-0026 accepted; criteria mapped to tests (T6) |
| 137 | `f3-8-c1-events` (draft, base: old main `ee269d9`) | F3.8 | execution policy events |
| 138 | `f3-8-c2-domain` (draft, base: 137) | F3.8 | secrets domain and validation |
| 139 | `f3-8-c3-redaction` (draft, base: 138) | F3.8 | redaction (F3.8 would move `draft` -> `in_progress`) |

Merge-order constraints: 153 -> 151 -> 145, and 139 -> 138 -> 137 are stacks
(merge bottom first, retarget the next onto `main`). 144/146-150/152/148 are
independent of each other but several edit the same status prose
(`docs/features/README.md`, spec headers, `docs/roadmap.md`), so expect conflicts
in documentation, not code.

## 2. Gate (precondition for task 1)

The gate opens when:

1. PRs 144-153 (non-draft) are merged or explicitly closed.
2. Draft PRs 137-139 are merged, closed, or the owner confirms they are left out
   and the ledger records F3.8 as whatever `main` then contains.
3. `main` is green (`make check`, `cd desktop && npm run check`).

Open the work on a fresh branch from the new `main`; carry the two files from
`83d6c07` (`docs/roadmap-ledger.yaml`, the audit report) onto it.

## 3. Owner decisions needed before the spec (task 0)

Recommendations in bold.

1. Two axes `status` + `coverage` instead of one column with a partial marker: **yes**.
2. The ledger replaces "status in spec headers" as the REL2/ADR-0031 source: **yes**.
3. Format: **YAML** (the YAML library is already a dependency) over JSON.
4. ADR-0027 (F3.2 implemented, Windows tests unrun) and ADR-0025 (implemented,
   macOS pending): accept now, or keep `proposed` and record the open part in
   `gaps`. **Accept both, with the open part recorded in `gaps`.**
5. Fate of the review's `docs/progress/*` files: **not recreated.** The ledger
   covers features, phases, demos and (task 6) the v1.0 and MVP criteria; the
   PDF's architecture-section tables stop being a status holder because each row
   is either a feature (already in the ledger) or a `gaps` entry.
6. Drafts 137-139: wait for them, or treat as out of the gate. **Wait if they
   merge within the week; otherwise exclude.**

## 4. Tasks

Each task is one small PR. Branch names follow `codex/LEDGER1-<slug>`.

### Task 0: Decisions, spec and ADR

- [ ] Record the decisions above in the spec.
- [ ] `docs/features/LEDGER1-roadmap-ledger.md` from `_template.md`, with
      measurable acceptance criteria (those listed per task below).
- [ ] `docs/adr/0032-roadmap-ledger-as-status-source.md` (confirm 0032 is still
      free after the open PRs merge). Documents: ledger is a durable contract,
      closed vocabulary, REL2 source change. Amends ADR-0031 by reference; does
      not rewrite it.
- [ ] Update `docs/adr/README.md` index.

Done when: spec and ADR reviewed by the owner and `accepted`.

### Task 1: Re-baseline the ledger on the new `main`

This is the only task that needs judgment. Do it as a reviewable diff of the
ledger, not as a rewrite.

- [ ] `git log --oneline fdaae01..main` and map every commit/PR to a feature ID.
- [ ] For each affected feature, read its spec acceptance criteria and the merged
      tests; set `status`, `coverage`, `gaps`.
- [ ] Re-verify the entries marked `source: review@4578233` that the merged work
      could touch, and ideally the P0 ones in phases 2-3. Switch them to
      `source: code` only after checking code and tests, never by assumption.
- [ ] Set `verified_head` and every touched `verified` to the new `main` short hash.
- [ ] Re-derive `phases[].state` and the demo C/D rows (C may move once F2.6/F2.7
      and F2.8b land).
- [ ] Add rows for any feature spec that appeared meanwhile (for example a F3.3
      spec if one was written) and any new `extra` item.

Done when: the ledger diff lists, per changed feature, the PR that justifies it.

### Task 2: Ledger checker in `make check`

Go, standard library plus the existing YAML dependency, under `scripts/ledgercheck/`
(not in `internal/`: it is developer tooling, not product code). Errors name the
file, feature ID and line.

Rules (each gets a failing-fixture test with the exact message):

- [ ] `schema_version` known; `status` and `coverage` in the closed vocabulary.
- [ ] IDs unique; every `F*.*` ID in `docs/roadmap.md` exists in the ledger and the reverse
      (until task 3 makes the tables generated).
- [ ] `spec` file exists; `implemented` and `in_progress` require a spec; `planned` forbids one;
      `draft` and `accepted` require one.
- [ ] `coverage` other than `full` requires non-empty `gaps`; `full` with `gaps` that claim
      missing scope is rejected only by review, not by the tool.
- [ ] `deps` refer to existing IDs (or `F3.exit`-style phase exits).
- [ ] The first word(s) of each spec's `**Status:**` line equal the ledger `status`
      (after task 5; before that, report as warnings).
- [ ] `verified` is a reachable commit; `source` is `code` or `review@<hash>`.
- [ ] `event_schema_version` equals `events.SchemaVersion` in code (single copy of that number).
- [ ] Phase `state` is consistent with feature statuses by the REL2 rule (done iff all P0
      are `implemented`).

Done when: `make check` runs it; breaking any rule above fails CI with an actionable message;
removing the ledger entry for a spec fails.

### Task 3: Generated views

- [ ] `scripts/ledgercheck -write` (or `make roadmap`) regenerates the tables inside marker
      comments in `docs/roadmap.md` (phase tables with status/coverage columns, answering review
      finding L2) and the status table in `docs/features/README.md`. Prose such as phase goals
      and exit-criteria text stays hand-written outside the markers.
- [ ] `make check` fails when committed output differs from regenerated output (same pattern
      as the desktop token sync check). Output is deterministic: no timestamps, sorted by ID.
- [ ] Optional `docs/roadmap-status.md` rendering of ledger + gaps for people who do not read YAML
      (replaces the unmerged `docs/progress/*`). Generated, never edited.

Done when: editing a status cell in a generated table fails `make check`.

### Task 4: Reduce `Status:` headers to the closed vocabulary

- [ ] Migrate the 40 spec headers: `**Status:** <vocabulary word>` on the first line, optional
      `**Public note:**` (REL2), and move the existing prose (task lists, owner decisions,
      verification notes) to a `## Status notes` section in the same file.
- [ ] Turn the task 2 header warning into an error.
- [ ] Specs of a completed feature get a one-line pointer to the ledger instead of narrative
      progress.

Done when: no spec header contains prose; the checker enforces equality with the ledger.

### Task 5: Remove duplicated claims

- [ ] `README.md` "Status": two sentences and a link to the ledger view; fix schema number by removing it.
- [ ] `AGENTS.md` "Current product phase": same reduction, keeping the architectural
      constraints that are not status (shared files are not isolated, F3.1 is not a sandbox, etc.).
      This file is the agents' instruction source, so the edit gets its own PR and explicit
      owner review.
- [ ] `docs/reference/implementation-queue.md` and `docs/reference/README.md`: banner "historical,
      not a status source"; drop the "demo pending" wording.
- [ ] `docs/architecture.md`: schema numbers stay only inside per-schema historical sections;
      delete restatements of "current schema".
- [ ] Plans under `docs/superpowers/plans/`: add `**Status:** completed | in progress | abandoned`
      header and tick the boxes of merged tasks (F3.2, F3.1, F2.8b, F3.4 as of the new `main`).
      Checker rule: a plan marked `completed` while its feature is not `implemented` is an error.
- [ ] ADR statuses follow the owner decisions in section 3.

Done when: `grep` for event-schema numbers and "pending demo" in the files above finds no stale claim.

### Task 6: Criteria sections in the ledger

- [ ] Add `v1_criteria` (10 rows from the Roadmapa section 13) and `mvp_must_have` (14 rows from
      Architektura section 14) with `state`, a `features` list of IDs and a one-line `why`.
      The checker verifies the IDs exist; a criterion cannot be `met` while one of its
      features is not `implemented`.

Done when: those two lists are generated into `docs/roadmap-status.md`.

### Task 7: Make the process enforce it

- [ ] AGENTS.md "Definition of done": a PR that changes a feature's status updates the ledger in
      the same PR.
- [ ] `.github/PULL_REQUEST_TEMPLATE.md`: checkbox "Ledger updated (or no status change)".
- [ ] `prompts/plan.md`, `prompts/review.md`, `CONTRIBUTING.md`: refer to the ledger.
- [ ] `scripts/new-feature.sh` adds a ledger row (status `draft`) for a new spec.

Done when: a new-feature dry run produces a valid ledger.

### Task 8: REL2 / public feed

- [ ] Amend REL2 requirement set: the generator reads the ledger (not spec headers and
      `roadmap.md`); `docs/roadmap.json` carries the same closed vocabulary plus `coverage`.
      REL2 itself stays `draft`; implementing the feed is not part of this plan.

Done when: REL2 and ADR-0031 no longer contradict the ledger.

## 5. Order and PR slicing

`0` (spec+ADR) -> `1` (re-baseline) -> `2` (checker) -> `3` (views) -> `4` (headers) -> `5`
(de-duplication) -> `6` -> `7` -> `8`. Tasks 6 and 8 may run in parallel after 3.
Tasks 4 and 5 touch many files; keep each one mechanical and reviewed against a
checker run, not by reading 40 diffs.

The checker lands in warning mode for rules that existing files would fail, and
becomes strict in the task that fixes those files, so `main` is never red.

## 6. Risks

- **Stale again on arrival.** New PRs keep merging while the plan runs. Mitigation: task 1 is
  repeated as a short delta right before task 2 merges; the checker's `verified` rule makes any
  later status change a conscious edit.
- **Conflicts on shared prose** (`docs/roadmap.md`, `docs/features/README.md`, spec headers) with
  the 13 open PRs. Mitigation: the gate, plus doing task 3 and 4 as the first changes after it.
- **Over-trust in `source: review@...`.** Those rows were never re-verified in code. Mitigation:
  task 1 re-verifies P0 rows in phases 2-3; the field stays visible.
- **Status vs coverage confusion.** A feature `implemented` with `coverage: partial` can read
  like a regression to people used to the old marker. Mitigation: the generated views print both
  columns and list `gaps`.
- **Heavy process for a solo/small team.** Mitigation: the only manual step is editing one row in
  one file; generators do the rest.

## 7. Verification

- `make check` and `cd desktop && npm run check` stay green at every step.
- Checker: one failing-fixture test per rule with exact error text; golden-file test on the real
  ledger.
- Generator: run twice, identical bytes; change one ledger status, regenerate, diff shows exactly
  that row in both views.
- Manual dry run: edit a status in `docs/roadmap.md` by hand and confirm CI would fail.
- Final audit: repeat the section 2 inventory of `docs/audits/2026-11-18-...`; every former
  status location is either generated, checked, or free of status claims.

## 8. Report format when finished

Per `AGENTS.md` step 6: behavior implemented, files changed, check results, known limits, and
follow-up outside scope (public feed, F3.3/F3.5/F3.7 specs listed in the ledger as `planned`
without a spec, which remains the largest planning gap).
