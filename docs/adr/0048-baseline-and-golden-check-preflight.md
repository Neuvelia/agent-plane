# ADR-0048 - Baseline and golden preflight for loop checks

**Status:** proposed
**Date:** 2026-11-27
**Feature:** F1.4c (issue #204)
**Extends:** ADR-0042 (test_command), ADR-0014 (loop); relies on ADR-0015 (acceptance only from a producer)

## Context

A `test_command` loop check (ADR-0042) only proves something if it is red
before the change and green after a correct change. Nothing verifies either
property today, so a loop can burn its whole attempt budget on a check that
was already green, broken (exit 126/127) or unable to recognize a correct
change.

The reference project in the adoption analysis
(`kacwojdev/agent-control-plane`) stops such units with zero model calls,
using four named reasons. It measured that naming the causes made most of its
wasted runs explainable.

## Decision

1. **Opt-in.** `loop.baseline: {require: red, golden_patch?: ABS_PATH}`, valid
   only for a `test_command` Gate. Without it nothing changes.
2. **Baseline run.** Before attempt 1, the Gate runs once with the producer's
   ordinary inputs and no candidate outputs. A Gate that cannot be evaluated
   without a candidate output is a validation error, not a silent skip.
3. **Classification.** The baseline is not an attempt and does not consume
   `max_attempts`.
   - Exit 126/127 or a start failure → `check_broken`.
   - Exit 0 → `baseline_green`.
   - Any other non-zero exit → red, and the loop proceeds.
4. **Golden proof.** With a red baseline and `golden_patch`, the patch is
   applied with `git apply` in a temporary linked worktree at `HEAD` (F3.1
   machinery) and the Gate runs there. A patch that does not apply gives
   `golden_broken`. A Gate that is not green gives `check_unproven`. The
   worktree is always removed, and the source checkout and index are never
   touched.
5. **All four outcomes are failures**, not successes: node `failed` with that
   `failure_kind`, run exit 4, and zero producer starts. A green baseline does
   **not** publish accepted outputs, because acceptance comes only from a
   producer attempt and a green Gate (ADR-0015).
6. **Event.** A new `baseline_check` event carries the baseline Check Result,
   the golden Check Result if any, and the classification (additive schema
   bump).

## Alternatives considered

### A. Always run a baseline

Pros:

- Catches bad checks everywhere.

Cons:

- Changes every existing loop.
- Doubles the check cost.
- Is wrong for checks whose input exists only after the producer runs.

### B. Green baseline = success ("already satisfied")

Pros:

- Re-running a graph on already-fixed repositories becomes free and green,
  which the reference project relies on for batch changes.

Cons:

- Publishes no producer output, or would have to invent one. Downstream nodes
  bound to producer outputs cannot run.
- Contradicts ADR-0015.

Can be revisited with a dedicated "satisfied" outcome and an explicit graph
rule for dependents.

### C. Treat exit 126/127 as `check_broken` everywhere, not only at baseline

Pros:

- Also catches a check broken by the change itself.

Cons:

- Amends ADR-0042 semantics inside the loop.

Recommended as a follow-up amendment, kept out of this decision so that
F1.4c stays additive.

## Consequences

### Positive

- Zero-token stops with typed, countable reasons.
- The golden proof turns "the check tests the change" from an assumption into
  evidence.

### Negative / trade-offs

- An extra check run per node, and a worktree per golden proof.
- A `workspace_writes` check may change the workspace before attempt 1. That is
  documented, not prevented (use F3.1 isolation or F3.9d confinement).

## Compatibility and migration

Additive. Graphs without `loop.baseline` are unchanged in hash and behavior.

## Validation

F1.4c acceptance criteria. After adoption, a count of the four failure kinds
across real runs shows how many attempts were saved.
