# ADR-0043 - Human reject routing and approval context

**Status:** accepted (owner, 2026-11-27)
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
   rejection. The "route" is the explicit data binding to a graph output, which
   records the verdict in the run result (rule 3 allows no other consumer).
3. Validation: `on_reject` must be `fail` or `continue` (`E_HUMAN_INVALID`).
   `continue` requires that `<node>.outputs.decision` reaches **graph outputs
   only**, and at least one; otherwise `E_HUMAN_REJECT_UNROUTED` (an error),
   reported once per node bound to the decision.

   Every node bound to the decision runs on rejection, so none of them can be
   the check:
   - a Shell or Agent node would let the rejected change through (for example
     a `deploy` step);
   - a loop node's producer runs before its embedded Gate judges anything;
   - no standalone Gate kind can compare the verdict string: `command`
     compares two files named by its inputs, `json_predicate` and
     `reported_file_scope` need JSON, `test_command` is loop-only and
     `changed_files_scope` needs a loop baseline.

   The graph output records the verdict for whoever reads the run result. A Gate
   kind that compares a string value would be a new contract (a later ADR) and
   could then be allowed here. The rule does not depend on field descriptors,
   so every validation path (CLI, API, runtime) applies it the same way.
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

- `internal/adapter/yamlgraph/human_test.go`: the values, and the routing rule
  (a graph output is accepted; a Shell, Gate or loop consumer, alone or beside
  a graph output, is rejected).
- `cmd/agentplane/validation_test.go`: a Gate that reads the decision is still
  rejected, and the approval-edge exemptions of `W_EDGE_INPUT_UNUSED`.
- `internal/runtime/human_test.go`: a rejection with `continue` publishes
  `"rejected"` on the decision; the default still fails.
- desktop `approval-context.test.tsx`.

**Owner action:** accepted by the owner on 2026-11-27 (PR #198).
