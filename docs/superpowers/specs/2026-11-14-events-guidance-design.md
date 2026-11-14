# H8: Events-local agent guidance

## Intent

An agent changing event contracts should find the right owner and regression
tests while preserving historical JSON compatibility, collection order and
bounded delivery. The root `AGENTS.md` remains in force.

## Approach

Add `internal/events/AGENTS.md` beside the event code. Point to the base
event feature and ADR plus the later progress, Check Result, attempt, unit,
changed-file and approval decisions. Describe the current schema-9 code and
distinguish the event package from runtime state transitions, store history
and API/desktop consumers.

This changes contributor guidance only. Product behavior, event JSON,
dependencies and CI rules stay unchanged.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide distinguishes newly emitted schema 9 from schemas 1-9 accepted
  by JSON readers, including the separate schema-4 Gate payload.
- It describes authoritative sequence gaps, retained versus live omission,
  terminal replay and optional ordered persistence accurately.
- It places redaction with producers before collection and identifies the
  package boundaries for state, persistence and delivery.
- A reviewer can find targeted tests for compatibility, concurrency,
  retention, copy isolation, typed facts and producer redaction.
- Focused event tests and `make check` pass.

## Risks

The base F0.6 spec and ADR describe schema 1 as implemented in that slice;
later accepted ADRs and current code extend this to schema 9. This guide
must avoid presenting historical phase text as the latest wire contract.
