# ADR-0034 - Canonical Agent adapter interface

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-26
**Source:** roadmap audit, decision D6; confirms ADR-0009

## Context

Roadmap F1.1 lists the operations detect capabilities, prepare, execute, stream
events, cancel and collect outputs. Architecture §8.1 lists `id`, `detect`,
`execute` (an async event stream) and `cancel(executionId)`. ADR-0009
implemented `ID`, `Detect` and `Execute`, optional progress interfaces for
streaming, cancellation through `context.Context` and outputs as the return
value of `Execute`.

## Decision

The ADR-0009 shape is canonical (`internal/agent/contract.go`). Mapping:

| Roadmap / architecture | Canonical |
| --- | --- |
| id | `ID()` |
| detect capabilities / detect | `Detect(ctx)` → `CapabilityReport` (tri-state) |
| execute | `Execute(ctx, input)` |
| stream events | optional `ExecuteWithNormalizedProgress` |
| cancel / cancel(executionId) | cancellation of `ctx` |
| collect outputs | the `AgentExecutionOutput` returned by `Execute` |
| prepare | not present; adding it needs a new ADR |

## Alternatives considered

### Add explicit Prepare and Cancel(id)

Cons: duplicates `ctx` cancellation and adds lifecycle state to every adapter
without a consumer.

## Consequences

The registry treats F1.1 as matching the canonical spec. A second adapter or
the adapter SDK (F6.5) must keep this shape or amend it by ADR.

## Compatibility and migration

None.

## Validation

`internal/agent/adaptertest` contract suite.
