# ADR-0043 - Human reject routing and approval context

**Status:** proposed
**Date:** 2026-11-26

## Context

F2.10 AC2 in the roadmap says a rejection "can end the run or route to an
explicitly configured path". ADR-0017 implemented only the first half: a reject
fails the node (`human_rejected`), skips dependents and fails the run (exit 4).
The roadmap scope also asks the approval panel to show checks, changed files, a
diff link and the reason for approval, plus a blast radius. Owner decisions
(Q17, D14, 2026-11-26): these are gaps, F2.10 stays `partial`, and the
blast-radius model moves to F3.9.

Graph schema 0 has no conditional edges: every dependent of an accepted node
runs, and an edge is a data dependency, not an ordering. A reject route must fit
that model rather than add control flow.

## Decision

1. Add an optional human `config.on_reject`, an enum of `fail` (default) and
   `continue`. Absent equals `fail`, which is the ADR-0017 behavior; graphs
   without the key keep their semantic version and every existing run is
   unchanged.
2. With `continue`, a rejection is recorded (`approval_decided` with decision
   `rejected`, unchanged payload) and the node ends `succeeded` with its
   `decision` output set to `"rejected"` instead of `"approved"`. No failure kind
   or `human_rejected` code exists for that node; the run is not failed by the
   rejection. The "route" is the explicit data binding: nodes that bind
   `decision` run and see the verdict as an ordinary string input, and a graph
   output may expose it. The author must judge the verdict with a
   deterministic check (rule 3).
3. Validation: `on_reject` must be `fail` or `continue` (`E_HUMAN_INVALID`).
   `continue` requires that `<node>.outputs.decision` reaches a **deterministic
   check**, otherwise `E_HUMAN_REJECT_UNROUTED` (an error). A deterministic
   check is one of:
   - a graph output;
   - a Gate node whose configuration reads the input that receives the decision;
   - a node whose loop forwards that input to its embedded Gate
     (`node.inputs.<id>` binding).

   A Shell or Agent node that merely receives the decision is not a check. It
   runs the same on rejection, so a rejected change would go through and the
   run would still succeed. Example: a `done` step that prints a fixed text.
   Which inputs a Gate reads comes from the field descriptors
   (`internal/fields`), supplied by the composition root. A caller without
   descriptors counts any Gate target.
4. The non-blocking `W_EDGE_INPUT_UNUSED` warning (#180) never fires on an
   approval edge: an edge from a human `decision` output or an edge into a
   human node. A decision edge into an Agent's otherwise unused input is what
   makes the Agent wait for approval. The approval panel shows every input of
   a human node. Following "remove the edge" there would remove the approval. The key is added to
   the human field descriptors (enum, default `fail`) so the inspector offers it.
5. Event schema is unchanged: `approval_decided` already carries the verdict. No
   schema bump is needed.
6. Panel context is desktop-only and derived from events already emitted: for the
   upstream nodes (from the graph summary) the human panel shows the latest check
   verdicts, the Git-observed changed files and a link that opens the upstream
   node, plus the node's prompt as the reason for approval. Nothing is inferred
   by the UI and nothing new is stored.

## Alternatives considered

### Alternative A - a second output port (`rejected`) with conditional skipping

Pros: only the chosen branch runs.
Cons: needs inactive-output semantics in the scheduler (dependents of an
unproduced output skipped, not failed), a change to ADR-0014 barriers and to
binding resolution. Too large for this slice; revisit with explicit conditional
control flow.

### Alternative B - `on_reject: <node id>` naming a target

Pros: obvious.
Cons: an ordering-only edge to a named node, which AGENTS.md rule 4 forbids.

## Consequences

### Positive

- Reject can end the run (default) or continue through an explicit,
  validated data path; old graphs and events are unchanged.

### Negative / trade-offs

- All dependents of the human node run on both outcomes, because schema 0 has no
  conditional edges. `continue` makes a rejected run succeed overall; the
  rejection is visible in `approval_decided` and in the `decision` value, not in
  run status or the exit code. Conditional branches are future work.
- Blast radius, and a diff body in the panel, are not delivered here (F3.9; the
  per-attempt diff panel is a separate feature, ADR-0026).

## Compatibility and migration

Graph schema stays 0 (additive optional config key; older engines reject the
unknown key with `E_HUMAN_INVALID`). Event schema unchanged.

## Validation

- `internal/adapter/yamlgraph/human_test.go`: the values, and the check rule
  (a Shell consumer is rejected; a Gate or a graph output is accepted).
- `cmd/agentplane/validation_test.go`: the descriptor-based checks, a Gate that
  ignores the decision, and the approval-edge exemptions of
  `W_EDGE_INPUT_UNUSED`.
- `internal/runtime/human_test.go`: a rejection with `continue` reaches its
  check as `"rejected"`; the default still fails.
- desktop `approval-context.test.tsx`.

**Owner action:** this ADR stays `proposed` until the owner accepts it. The
code ships under it, following the ADR-0028/ADR-0029 precedent.
