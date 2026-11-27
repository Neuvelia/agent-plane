# Roadmap tracking

This directory tracks the roadmap's state against the code. It was decided in
ADR-0038 after the 2026-11-25 audit (`audit/AUDIT-2026-11-25.md`).

| File | Role | Edited by |
| --- | --- | --- |
| [`features.yaml`](features.yaml) | **Single source of feature status**: every roadmap feature F0.1–F6.5, derived entries, enablers and future entries; acceptance criteria with IDs, status and evidence | hand, in the PR that changes the feature |
| [`phases.yaml`](phases.yaml) | phases, exit criteria and their status, demo milestones A–E | hand |
| [`STATUS.md`](STATUS.md) | readable status: progress per phase and priority, exit criteria, demos, open criteria, divergences | **generated** (`make roadmap`), never by hand |
| [`canonical-spec.md`](canonical-spec.md) | the one binding version of names, states, events, graph format, adapter interface, layout | an ADR for every change |
| [`decisions/`](decisions/README.md) | index of the ADRs that settle roadmap questions (the ADRs live in `docs/adr/`) | with each new ADR |
| [`issue-triage.md`](issue-triage.md) | the `claude-ready` / `needs-owner` rule for GitHub issues and its enforcement workflow | hand |
| [`audit/`](audit/) | audit reports `AUDIT-<YYYY-MM-DD>.md` | each re-audit |

The sources are `docs/reference/full-roadmap-features.md` (the roadmap) and
`docs/reference/full-architecture-plan.md` (the architecture plan). Both are
verified copies of the original PDFs, which stay outside the repository. The
canonical spec overrides both where it decides something. The roadmap decides
phase scope, priorities and acceptance criteria.

Spec status lines (`docs/features/*.md`), `docs/roadmap.md` and the
narrative in AGENTS.md/README are informative. When they disagree with
`features.yaml`, the registry wins and the disagreement is a documentation bug
to fix.

## Commands

```bash
make roadmap      # validate the registry and regenerate STATUS.md
```

```bash
make check        # includes `go run ./scripts/roadmapcheck`: fails on registry errors or a stale STATUS.md
```

`scripts/roadmapcheck` exits non-zero on **errors** and only prints
**warnings**.

Errors:

- invalid schema, an unknown field, or a value outside a vocabulary;
- a duplicate feature or criterion ID;
- an unknown dependency, or a dependency cycle;
- `done` while a criterion is `open` or has no evidence;
- an evidence file or named test that does not exist;
- a `met` criterion whose evidence test files carry no tag for it;
- a divergence without `adr`, `issue` or `deferred_to`;
- a missing ADR or spec file;
- a stale `STATUS.md`.

Warnings:

- a `done` feature whose dependency is not `done`/`diverged`;
- a test tag naming no registered criterion (an orphan).

CI runs the check through `scripts/check.sh` in the Go lane. Changes under
`docs/roadmap/`, `docs/features/` or `docs/adr/` select that lane.

## Registry schema (`features.yaml`)

```yaml
- id: F1.7                      # roadmap ID; derived: F1.4b, F2.3-E6; enabler: REL1; future: FX.1
  name: "Return Envelope / scoped retry"
  kind: roadmap                 # roadmap | derived | enabler | future
  parent: F1.6                  # derived only
  phase: 1                      # 0-6; null for enabler/future
  priority: P0                  # P0 | P1 | P2
  status: done                  # not_started | in_progress | partial | done | diverged | deferred
  owner_package: execution-core # roadmap §10 area; directories in canonical-spec.md §7
  spec: docs/features/F1.7-return-envelope-scoped-retry.md
  depends_on: [F1.5, F1.6]
  acceptance:                   # copied 1:1 from the roadmap; derived entries may have none
    - id: F1.7-AC1
      text: "W teście A/B/C/D po błędzie C ponownie wykonuje się wyłącznie C."
      status: met               # open | met
      evidence:                 # path, or path::TestName, or path::<criterion ID> (vitest tag)
        - "internal/runtime/unit_integration_test.go::TestUnitsBarrierScopedRetryAndExhaustion"
      issue: 178                # optional, for an open gap
      note: "..."               # optional
  evidence: []                  # feature-level, for entries without acceptance criteria
  out_of_scope: ["Cross-unit replanning ..."]
  decisions: [ADR-0015]
  divergences:                  # each needs adr, issue or deferred_to
    - text: "..."
      adr: ADR-0024
      issue: 189
      deferred_to: F3.9
  tasks: [178]                  # GitHub issue numbers
  note: "..."
```

**Status rules** (ADR-0038):

- `done`: every criterion is `met` with evidence.
- `diverged`: every criterion is met, but an ADR changed the scope and
  `canonical-spec.md` does not adopt that change.
- `partial`: some criteria or scope met, with open criteria or open gaps
  (issues).
- `in_progress`: work started, criteria not met.
- `deferred`: consciously postponed (future entries).
- There is no `unknown`: investigate, or use `in_progress` with a note.

A divergence without an ADR is **a gap**: an open criterion or a divergence
with an issue. Every deferral names its target (`deferred_to`).

## Traceability conventions

**Tag the test that proves a criterion**:

- Go: a comment directly above the test function.

  ```go
  // roadmap: F1.7-AC1, F1.7-AC2
  func TestUnitsBarrierScopedRetryAndExhaustion(t *testing.T) {
  ```

- vitest: the criterion ID in brackets in the `it()` name:
  `it('keeps the verdict, reason and evidence on the node [F2.7-AC1]', ...)`.
- e2e (`desktop/e2e/demo.cjs`): a comment `// roadmap: F2.9-AC1` before the
  step.

Then list the test in the criterion's `evidence`. A rename breaks the check
in both directions: a missing test is an error, and an orphan tag is a
warning.

**Commits and pull requests** reference the feature ID:

- commit subject: `feat(runtime): [F1.7] ...`, `fix(graph): [F0.1] reject unknown node types (#178)`;
- PR title prefix: `[F1.7] ...`;
- the PR template's "Feature ID" field.

**Decisions:** any change to scope, a contract or the canonical spec is an ADR
(`scripts/new-adr.sh <slug>`) plus a row in `decisions/README.md`. Never
change the spec silently.

**Tasks** are GitHub issues. Their numbers go into `tasks` and into
criterion/divergence `issue` fields. Every issue carries `claude-ready` or
`needs-owner` ([`issue-triage.md`](issue-triage.md)).

## Workflow for a feature, fix or docs change

1. Read the feature's entry in `features.yaml`, its spec, `canonical-spec.md`
   and the linked ADRs. Stay inside `out_of_scope`; widening it needs an ADR.
2. Implement. Add or tag tests for the criteria you prove.
3. Update `features.yaml`:
   - criterion `status` and `evidence`;
   - feature `status`;
   - `divergences`, `tasks` and the issue references.
4. Run `make roadmap`, then `make check`. Commit `STATUS.md` with the change.
5. Name the feature ID in the commit and the PR.

The reusable prompts in `prompts/` (see `prompts/README.md`) encode this
workflow for agents.

## Automation and dashboard

Two scheduled Claude Code tasks run on the default branch. Each fire is a fresh
session, and its full instructions live in the task itself:

| Task | When (Europe/Warsaw) | What it does |
| --- | --- | --- |
| Roadmap daily check | every day, 06:42 | Runs `go run ./scripts/roadmapcheck -export DIR -revision <sha>`, refreshes the dashboard's data files, and reports errors, warnings and the day's roadmap changes. It changes nothing in the repository. |
| Roadmap weekly re-audit | Mondays, 07:39 | Runs the incremental re-audit below with `prompts/re-audit.md` and opens a `[roadmap] audit <date>` PR. It never merges and never changes product code. |

The **dashboard** is a private claude.ai artifact of type Data, owned by the
repository owner. It reads five JSON files that `roadmapcheck -export` writes:

- `features.json`
- `criteria.json`
- `phases.json`
- `demos.json`
- `divergences.json`

Each file is `{rows, meta}`, and `meta` carries the revision. The dashboard is
a view, never a source of truth. A stale dashboard means the daily task did not
run; it does not mean the status changed.

```bash
go run ./scripts/roadmapcheck -export /tmp/roadmap-export -revision "$(git rev-parse --short HEAD)"
```

## Re-audit procedure

Run it on demand, or before a phase is declared done. Use
`prompts/re-audit.md` for an agent.

1. **Baseline.** Read the latest `audit/AUDIT-*.md`, `features.yaml`,
   `phases.yaml` and `canonical-spec.md`. Run `make roadmap`, `make check`
   and `git log --since=<date of last audit>`.
2. **Registry vs code.** For every feature that is not `done`/`not_started`,
   and every feature touched since the last audit, verify each criterion
   against code and tests:
   - status comes from evidence, never from spec text;
   - a `met` criterion needs a tagged test.
3. **Canonical spec vs code.** Check:
   - the CLI usage string;
   - the state enums;
   - the event types and the current schema version;
   - the graph fields;
   - the adapter interface.
4. **Invariants and cross-cutting requirements.** Go through
   `canonical-spec.md` §8 and the roadmap §5:
   - no secrets in logs, events or exports;
   - idempotency;
   - scheduler determinism;
   - no scheduler semantics in the UI;
   - no arbitrary-command endpoint;
   - unknown is not zero.
5. **Docs vs registry.** Look for stale spec status lines, roadmap tables,
   AGENTS.md and README.
6. **Write** `audit/AUDIT-<YYYY-MM-DD>.md`. It must contain:
   - a summary;
   - a section **"Changes since the previous audit"**: what improved, what
     newly diverged, what is unchanged;
   - findings with `path:line`;
   - the `needs_decision` items.

   Put new `needs_decision` items in `features.yaml` (`needs_decision:`).
7. **Propose** registry updates and doc fixes in a PR. Do not change product
   code in a re-audit.

The diff against the previous audit comes from comparing the two reports'
feature tables and `git diff <old-audit-commit> -- docs/roadmap/features.yaml`.
