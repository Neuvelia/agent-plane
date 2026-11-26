# ADR-0032 - Canonical vocabulary: CLI name, state model and event names

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-26
**Source:** roadmap audit `docs/roadmap/audit/AUDIT-2026-11-25.md`, decisions D1, D2/D3, D4

## Context

The two source documents (`docs/reference/full-roadmap-features.md`, the
roadmap, and `docs/reference/full-architecture-plan.md`, the architecture plan)
disagree on the CLI name (`agentplane` vs `agentctl`), on node and run states
(roadmap F0.4 `pending|ready|running|succeeded|failed|skipped|cancelled`,
F1.6 `failed_needs_intervention`, F2.5 UI statuses, vs architecture §6.1/§6.2
`PENDING|READY|RUNNING|CHECKING|CORRECTING|WAITING_FOR_HUMAN|FAILED|CANCELED`
and run `QUEUED|INTERRUPTED`) and on event names (snake_case vs PascalCase).
The code already implements the roadmap variant.

## Decision

1. **CLI name:** `agentplane`. The architecture's `agentctl` is superseded. A
   rename (for example to `tarmind`, REL1) happens only through REL1 and an ADR.
2. **Two-level state model.**
   - Domain node states (`internal/domain/run/state.go`): `pending`, `ready`,
     `running`, `succeeded`, `failed`, `skipped`, `cancelled` (British spelling).
   - Domain run states: `running`, `succeeded`, `failed`, `cancelled`.
   - An exhausted correction loop is node `failed` with failure kind
     `needs_intervention`; the roadmap's `failed_needs_intervention` maps to it.
   - Checking, correcting and waiting for a human are phases visible through
     events (`attempt_*`, `gate_check`, `approval_requested`/`approval_decided`),
     not domain states.
   - Presentation statuses (`retrying`, `waiting_human`, `interrupted`, …) are
     derived by readers from events or the store lock (F3.2) and never written
     as events. The roadmap F2.5 status names map to them (canonical spec).
   - `queued` is not a state until a feature that queues runs (F4.6/F5.2) adds
     it through an ADR.
3. **Event names:** snake_case. The catalog in `internal/domain/events`
   (event schema 14 at the time of this ADR) is canonical. The architecture's
   PascalCase names are illustrative and are mapped in
   `docs/roadmap/canonical-spec.md`.

## Alternatives considered

### Adopt the architecture-plan states and PascalCase events

Pros: one naming scheme with the architecture document.

Cons: changes every stored event stream and the desktop reducer, needs a
migration, and adds states (`CHECKING`, `CORRECTING`) that duplicate facts the
events already carry.

## Consequences

### Positive

- Code, docs and the canonical spec agree; no migration.

### Negative / trade-offs

- Readers must map architecture-plan terms through the canonical spec.

## Compatibility and migration

None. This records existing behavior.

## Validation

`docs/roadmap/canonical-spec.md` lists the mapping; `scripts/roadmapcheck`
and the event tests keep the catalog in code.
