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

Waiting is not a scheduler state. The coordinator owns node transitions and is
blocked on the current batch, so a worker cannot move a node into a new state.
A human node runs as a normal worker: it emits `approval_requested`, then blocks
in a consumer-owned `DecisionProvider.Decide(ctx, ApprovalRequest)` and returns
the outcome. "Waiting for a human" is derived from events
(`approval_requested` without `approval_decided` for the node run). The node
holds one concurrency slot while waiting and the batch barrier of ADR-0014 is
unchanged: nodes in the same batch keep running, later-ready nodes start after
the batch. An event-driven scheduler that yields the slot is deferred.

Add events `approval_requested` and `approval_decided` (event schema 9,
additive; decode of 1-8 unchanged). Payloads carry node run ID, bounded
prompt/context/comment and the decision; redaction applies before emission.

The scheduler calls the provider once per node run. A provider serving several
channels owns first-decision-wins; repeats are no-ops that return the original
decision. Reject fails the node (kind `rejected`, code `human_rejected`), skips
dependents, exits 4. Cancellation while waiting yields `cancelled` with no
decision event. A graph containing a human node and no provider fails
preflight. No timeout and no approver identity in this iteration.

## Dependency: golang.org/x/term

The interactive CLI prompt must only run on a real terminal. `/dev/null` and
`/dev/zero` are character devices, and the standard library has no portable
terminal test, so a stdlib heuristic either prompts on a non-terminal (and can
hang or grow without bound reading NULs) or needs per-OS ioctl code.
`golang.org/x/term` (Go team, BSD-licensed) provides `IsTerminal` for Linux, macOS
and Windows. It is a **runtime** dependency of the CLI, pulls in
`golang.org/x/sys` (indirect), opens no network connections and reads no data. It
is pinned to v0.27.0 because later releases raise the module's `go` directive
above the project's 1.23.

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

- The wait ends with `approval_decided`, or - when the provider fails or the run
  is cancelled with no decision - only with the node's terminal event. Consumers
  must treat `node_finished`/`node_terminated` as also ending the wait.
- A decision a provider already accepted is recorded as `approval_decided` even
  if cancellation arrives at the same moment; the node then ends `cancelled`.
  A client that was told "recorded" therefore always finds the event.
- Interactive CLI prompts are serialized: one question at a time, and an answer
  is attributed to the node it was asked for.
- A run can wait indefinitely until decision or cancellation.
- A waiting human node holds a concurrency slot and later-batch nodes wait for the batch barrier.
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
