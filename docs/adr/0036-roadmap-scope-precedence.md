# ADR-0036 - Roadmap precedence for phase scope; units, blast radius and ordering

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-26
**Source:** roadmap audit, decisions D7/D8, D13, D14

## Context

The architecture plan §18 omits secrets (F3.8) and execution policy (F3.9)
from Phase 3, and §21.6/§23 order the Run Inspector before the graph editor,
while the roadmap lists both editor and inspector as Phase 2 P0. The
architecture §11 example uses dynamic fan-out (`foreach`); ADR-0015 implements
static units only. Both documents require a blast-radius model (roadmap §2 and
F2.10, architecture §10.2 L1–L3); the repo has none (F2.10 non-goal).

## Decision

1. **Phase scope and priorities come only from the roadmap.** F3.8 and F3.9
   are Phase 3 P0. From the architecture plan the project keeps one ordering
   rule: close F2.7 (Run Inspector node detail) before further graph-editor
   work.
2. **Units:** static units (ADR-0015) are accepted for F1.7. Dynamic fan-out
   (`foreach` / Splitter) is registered as a future, unscheduled feature
   (`FX.1` in `docs/roadmap/features.yaml`), not as a gap in F1.7.
3. **Blast radius:** the consequence-based model (classes such as L1–L3) is
   part of F3.9 (execution policy v0). F2.10 stays `partial` with its panel
   context deferred to F3.9 (issue #187).

## Consequences

- F3.9's spec must add the blast-radius model; F2.10 depends on it.
- Graph-editor work after this ADR should not precede F2.7.

## Compatibility and migration

None.

## Validation

The registry dependencies (`F2.10` → `F3.9`) and STATUS.md make the ordering
visible.
