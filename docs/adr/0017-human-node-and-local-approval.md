# ADR-0017 - Human node and local approval decision

**Status:** accepted
**Date:** 2026-11-13

## Context

Graph units finish only on deterministic evidence. A run needs a first-class
node that stops a branch for an explicit human decision, records it as a fact,
and stays inside the local-first, DAG and bounded-execution rules. F2.10 needs
a pause/approval contract before any UI is built.

## Decision

Add node `type: human` to graph schema 0 (additive; nodes without it hash
unchanged). It declares output `decision` (string); approve publishes
`"approved"` as an ordinary accepted output so downstream dependence is a real
data edge.

Add runtime state `waiting_human`. A human node is not an Executor slot holder:
it registers a pending decision with a consumer-owned `DecisionProvider`
(`Decide(ctx, Request) (Decision, error)` semantics via a keyed pending-set) and
yields its concurrency slot while waiting. The coordinator owns transitions.

Add events `approval_requested` and `approval_decided` (event schema 9,
additive; decode of 1-8 unchanged). Payloads carry node run ID, bounded
prompt/context/comment and the decision; redaction applies before emission.

First decision per node run wins. Repeats are no-ops and return the original
decision. Reject fails the node (kind `rejected`, code `human_rejected`), skips
dependents, exits 4. Cancellation while waiting yields `cancelled` with no
decision event. A graph containing a human node and no provider fails
preflight. No timeout and no approver identity in this iteration.

## Alternatives considered

### Alternative A - human decision as a Gate verdict

Pros: reuses Gate/Check Result.
Cons: Gates are deterministic and read-only by contract (ADR-0012); mixing a
human wait into them breaks that guarantee and the Check Result schema.

### Alternative B - ordering-only control edge

Pros: simple.
Cons: violates the data-dependency edge rule; hides what the decision gates.

### Alternative C - block inside a Shell command (stdin prompt)

Pros: zero engine change.
Cons: invisible to events/UI, not auditable, holds a slot and a process.

## Consequences

### Positive

- Auditable, observable approval with explicit data dependency.
- UI and CLI share one decision seam.

### Negative / trade-offs

- A run can wait indefinitely until decision or cancellation.
- A waiting run does not survive process death (F3.3 later).
- Approver identity and roles are absent until Team scope.

## Compatibility and migration

Graph schema stays 0; event schema 8 -> 9 additive. Old graphs, events and
semantic hashes unchanged. Consumers of schema 9 must tolerate unknown payload
fields.

## Validation

Spec F2.10 acceptance tests: wait-visible-before-decision, parallel branch
progress while waiting, approve/reject/duplicate/cancel, preflight without
provider, schema 1-8 decode, `make check` and race run.
