# ADR-0018 - Correction loop for Shell producers

**Status:** accepted
**Date:** 2026-11-13

## Context

ADR-0014 limits loops to Agent producers with correction feedback. To test and
demo scoped retry deterministically, and to give deterministic tools the same
produce -> check -> correct unit, a Shell producer needs the same bounded loop.

## Decision

Allow `loop` on `shell` nodes in addition to `agent`. All other ADR-0014 rules
are unchanged: bounds 1-10 default 3, exactly one embedded Gate, typed bindings
from `candidate.outputs.PORT`, unique AttemptIDs under one NodeRunID, accepted
output withheld until green, only a valid red retries, exhaustion is
`needs_intervention` (exit 4), cancellation checked before dispatch/check/retry.

Shell has no correction-feedback channel: the command is re-run unchanged.
`allowed_paths` is not required for shell producers. Each attempt gets
`AGENTPLANE_ATTEMPT` (1-based) via the explicit environment mechanism so
behavior per attempt is deterministic. This is the only new reserved variable.

A non-zero shell exit remains an operational fatal failure, not a red verdict.
Reuse the existing loop coordinator; the executor-capability preflight for
shell requires only that a Gate and observed-check support exist.

## Alternatives considered

### Alternative A - fake agent adapter only

Pros: no schema/validation change.
Cons: demo depends on agent machinery and its feedback/scope requirements;
deterministic tools get no correction semantics.

### Alternative B - retry on non-zero exit

Pros: simple flaky-command retry.
Cons: hides broken configuration and processes; contradicts ADR-0014's fatal-error rule.

### Alternative C - separate shell-only retry construct

Pros: no coupling to Agent code.
Cons: duplicates attempt/check/event semantics; two retry models.

## Consequences

### Positive

- Deterministic demos and tests of scoped retry without an LLM.
- One loop semantics for all producer types.

### Negative / trade-offs

- Shell side effects of rejected attempts persist (no isolation until F3.1).
- Command authors must make attempts idempotent or attempt-aware.

## Compatibility and migration

Event schema unchanged by this ADR; graph schema 0 additive; nonloop shell and
Agent graphs keep behavior and semantic hashes. New environment variable name
is reserved and must not be user-overridable.

## Validation

F1.6a acceptance tests plus unchanged ADR-0014 suites; sibling-isolation test;
`make check` and race run.
