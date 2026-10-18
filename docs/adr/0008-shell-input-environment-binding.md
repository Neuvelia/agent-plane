# ADR-0008 - Explicit Shell input environment mapping

**Status:** accepted
**Date:** 2026-10-18

## Context

ADR-0005 intentionally left resolved node inputs unused by Shell execution.
The Phase 0 three-node example could therefore show binding readiness, but
its dependent process could not use the values supplied by its predecessors.

## Decision

Shell configuration gains an optional `input_env` mapping from declared
string node input names to child environment variable names. The executor
copies only present resolved values, without expression parsing or implicit
type conversion. A required mapped input missing from a direct executor call
fails before process launch. Unknown or non-string input names, malformed or
duplicate target names, and collisions with `env` or `env_allowlist` also fail
before launch. Target-name comparisons ignore case for cross-platform
predictability.

## Alternatives considered

- Appending values to argv would expose them in process listings and tie the
  mapping to each command's argument grammar.
- String interpolation would introduce an expression language and escaping
  rules beyond Phase 0.

## Consequences

The graph can carry data from branch outputs to a dependent Shell process and
produce an observable final result. Inputs remain explicit and isolated from
the child's environment by default. A process can still print a mapped value;
F0.6 redaction must be configured explicitly when that value is sensitive.

## Compatibility and migration

Graphs without `input_env` retain ADR-0005 behavior. This ADR extends the
Shell configuration contract; it does not change graph bindings, scheduler
state, event types, or CLI JSON schema.

## Validation

Executor tests cover value delivery and invalid mappings. The built CLI runs
the three-node example with two different graph inputs and verifies both
upstream values in the dependent output, ordered events, and terminal success.
