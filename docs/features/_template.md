# {{FEATURE_ID}} - {{FEATURE_NAME}}

**Status:** draft
**Priority:** P0/P1/P2
**Target phase:**
**Registry:** `docs/roadmap/features.yaml` entry {{FEATURE_ID}}

## Problem

What concrete problem does this feature solve?

## Goal

What observable capability exists after the feature is complete?

## Non-goals

- Explicitly excluded behavior.

## Functional requirements

1. Requirement.
2. Requirement.

## Data / API contracts affected

List graph schema, runtime state, events, CLI, persistence, or other public contracts.

## Failure behavior

Describe important invalid inputs and runtime failures.

## Roadmap criteria

Every roadmap criterion of this entry (`docs/roadmap/features.yaml`), with how this
spec treats it: covered, narrowed, redefined or deferred. Name the ADR or the target
feature ID for anything that is not covered (ADR-0038: no silent narrowing).

| Criterion | Roadmap text | Treatment |
| --- | --- | --- |
| {{FEATURE_ID}}-AC1 | ... | covered by criterion 1 |

## Acceptance criteria

- [ ] Measurable criterion (proves {{FEATURE_ID}}-AC1; test tagged `// roadmap: {{FEATURE_ID}}-AC1`).
- [ ] Measurable criterion.

## Required tests

- Unit:
- Integration:
- CLI/e2e:

## Security / trust considerations

What inputs are untrusted? What must not be logged or widened?

## Observability

What events/evidence must be emitted?

## Open questions

- None, or list decisions that must be resolved before implementation.

## Out of scope for this iteration

- Explicitly deferred behavior.
