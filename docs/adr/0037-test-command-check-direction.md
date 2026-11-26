# ADR-0037 - Direction: a test command as a loop check

**Status:** accepted (owner, 2026-11-26) as a direction; the contract needs a feature spec
**Date:** 2026-11-26
**Source:** roadmap audit, decision D12; related ADR-0012, ADR-0018

## Context

The roadmap F1.4 AC1 ("a failing test gives a red verdict and blocks
downstream"), the Phase 1 exit criterion and Demo B, and the architecture plan
(§5.7 "test suite exits 0", §11 `check: {type: shell, run: pytest}`) all assume
that a test command can judge a unit inside its correction loop. ADR-0012 keeps
Gates read-only and limits the `command` kind to `/usr/bin/diff`; a non-zero
Shell exit inside a loop is fatal and not retried (ADR-0018).

## Decision

The gap is to be **closed**, not accepted as a divergence. A new feature
(`F1.4b` in the registry, issue #183) will specify a loop check that runs a
test command: exit ≠ 0 is a red verdict with bounded evidence (check id, exit
code, shortened log), with an explicit side-effect and workspace policy that
keeps the deterministic, bounded and traceable rules of AGENTS.md. Until it is
delivered, F1.4 is `diverged` in the registry.

## Alternatives considered

### Accept the ADR-0012 divergence

Cons: the central "test fails → agent corrects only its unit" scenario of both
documents stays inexpressible.

## Consequences

The F1.4b spec must decide how a test command differs from a Gate (Gates do
not modify the workspace) and how ADR-0012 is amended.

## Compatibility and migration

Additive: a new check kind; existing Gates are unchanged.

## Validation

F1.4b acceptance criteria, including a red-then-green loop driven by a real
test command.
