# ADR-0038 - Roadmap tracking harness: single status registry and traceability

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-26
**Source:** roadmap audit, decisions Q2-Q10, Q17
**Amends:** ADR-0031 decision 2 ("Status stays in the specs")

## Context

Feature status was kept by hand in five places (spec `**Status:**` lines,
`docs/features/README.md`, `docs/roadmap.md`, AGENTS.md, README) that
contradicted each other; about 30 stale statements were found on 2026-11-25.
Acceptance criteria had no stable IDs and tests were not linked to them.
ADR-0031 proposed generating a public feed from the free-text spec status lines.

## Decision

1. **Single source of status:** `docs/roadmap/features.yaml`. It lists every
   roadmap feature (F0.1-F6.5), derived entries (`parent`), enablers and future
   entries, each acceptance criterion with a stable ID (`F1.7-AC1`), its status
   and evidence. `docs/roadmap/phases.yaml` holds phases, exit criteria and demo
   milestones. `docs/roadmap/STATUS.md` is generated and never edited by hand.
   Spec status lines and roadmap tables are informative; where they disagree,
   the registry wins and the disagreement is a bug. The ADR-0031 feed
   (`docs/roadmap.json`) will be generated from the registry, not from spec lines.
2. **Status vocabulary:** `not_started | in_progress | partial | done | diverged | deferred`
   (no `unknown`). `done` requires every criterion `met` with evidence.
   `diverged` means every criterion is met but the scope differs from the
   roadmap by an ADR that the canonical spec does not adopt. An open gap or an
   unmet criterion is `partial` or `in_progress`.
3. **Silent divergences are gaps.** A divergence without an ADR is an open
   criterion or a gap with a GitHub issue. Every deferral names its target
   feature ID (`deferred_to`).
4. **Traceability:** a test proving a criterion carries its ID:
   Go `// roadmap: F1.7-AC1` above the test function; vitest `[F1.7-AC1]` in
   the `it()` name; the e2e script a `// roadmap: F2.5-AC2` comment. Evidence
   entries are `path` or `path::TestName`.
5. **Decisions are ADRs** in `docs/adr/` (one numbering);
   `docs/roadmap/decisions/README.md` is only an index of roadmap decisions.
   Audits live in `docs/roadmap/audit/`.
6. **Tasks are GitHub issues**, referenced by number in the registry.
7. **Check:** `scripts/roadmapcheck` (Go, standard library plus the existing
   YAML dependency) validates the registry, evidence and tags, and regenerates
   or verifies `STATUS.md`. It runs inside `scripts/check.sh` (so in `make check`
   and CI); errors fail, warnings do not.
8. Commits and pull requests reference the feature ID (`[F1.7] ...`).

## Alternatives considered

### Keep status in spec lines with a closed vocabulary (ADR-0031 as proposed)

Cons: no per-criterion status, no evidence, sub-IDs and enablers do not fit,
and the free text of 36 specs would need rewriting anyway.

## Consequences

### Positive

- One place to update; CI catches `done` without evidence, missing tests,
  dependency cycles and a stale STATUS.md.

### Negative / trade-offs

- Registry upkeep is part of every feature PR.
- `scripts/roadmapcheck` reads Markdown-adjacent files, so CI must run the Go
  lane when `docs/roadmap/` changes (`.github/workflows/ci.yml`).

## Compatibility and migration

REL2 and ADR-0031 must take their input from the registry before they are
implemented.

## Validation

`make check` runs the check; `docs/roadmap/README.md` describes the workflow
and the re-audit procedure.
