# ADR-0051 - Resume and recovery semantics for interrupted runs

**Status:** proposed
**Date:** 2026-11-27
**Feature:** F3.3 (issue #207)
**Builds on:** ADR-0027 (event log is the durable state; `interrupted` is derived), ADR-0017 (human node), ADR-0024 (worktree isolation), ADR-0015 (immutable acceptance)

## Context

ADR-0027 made the event log the run's durable state and deferred recovery to
F3.3. ADR-0017 accepted that a waiting human node dies with its process. The
roadmap requires that completed nodes are not executed again after a restart
(F3.3-AC1), and that an uncertain command with a side effect is not replayed
automatically (F3.3-AC2). Two facts make this non-trivial:

- **Accepted values are not in the log.** F2.3-E6 values are opt-in, bounded
  and redacted, so they cannot rehydrate inputs.
- **The log cannot tell** whether a started node's side effects happened.

## Decision

1. **Resume is explicit and same-ID.** `agentplane runs resume RUN_ID` (and the
   API `POST /runs/{id}/resume`) applies only to a derived `interrupted` run.
   It appends to the same log; there is no new run ID and no automatic resume.
2. **The graph must be identical.** The recorded graph file is re-read and its
   semantic `graph_version` must match. Otherwise resume is refused. Approvals
   and acceptance never carry over to a different graph.
3. **Classification is a pure function of the log.**
   - Succeeded nodes are **kept** and never executed.
   - Nodes never started are **pending**.
   - Nodes started without a terminal event are **uncertain**: they become
     `needs_intervention` with reason `uncertain_side_effect`, and run again
     only with an explicit `--retry NODE`, under a new node-run ID linked to
     the old one.
   - Waiting human nodes return to waiting and can be decided with
     `--approve` / `--reject`.
   - Failed and skipped nodes are unchanged.

   There is no `recovery: replay` declaration in v1.
4. **Values are persisted separately from events.** Runs started with
   `--resumable` write each accepted output once as
   `values/SHA256.json` (mode 0600) in the run directory. The acceptance event
   records `value_ref`. Resume verifies the hash before use. Missing or corrupt
   values refuse resume and never trigger silent recomputation.
5. **Isolated units:** kept units are already integrated on the run-scoped ref.
   An uncertain unit's worktree is discarded and never integrated.
6. **Events:** `run_resumed` is appended first, with the runtime version and the
   node lists. The `needs_intervention` reason gains `uncertain_side_effect`.
   This is an additive schema bump.
7. **Every refusal leaves the log byte-identical.** Refusals are checked before
   any append, under the run lock.

## Alternatives considered

### A. Replay uncertain nodes automatically

Pros:

- Fewer operator steps.

Cons:

- Violates F3.3-AC2.
- Duplicates side effects: appends, pushes, Agent edits.

### B. Store accepted values inside events

Pros:

- One file.

Cons:

- Raw values (possibly sensitive, possibly large) enter the trace. That
  contradicts the event rules ("never place raw secrets in events") and the
  bounded-event design.

### C. Persist values for every run by default

Pros:

- Every run is resumable.

Cons:

- Sensitive outputs land on disk for every run.
- Disk use grows.

Kept as an open owner decision; opt-in is the recommendation.

### D. Resume as a new run that copies kept results

Pros:

- No change to closed logs.

Cons:

- Splits one logical run across two IDs.
- Approvals and history become harder to follow.
- Contradicts "accepted output is immutable for the current run".

## Consequences

### Positive

- A crash or a late approval no longer costs completed work.
- Uncertainty is visible and decided by a person.

### Negative / trade-offs

- Only runs started with `--resumable` can resume past a kept node with
  pending dependents.
- Value files are a second sensitive artifact per run and must be excluded
  from export (F3.7) and diagnostics (F3.11).
- The F3.2 reducer and the desktop must understand a log with more than one
  process segment.

## Compatibility and migration

Additive. Runs without `--resumable` behave as today, and resume is refused
for them when values would be needed. Older logs are readable but cannot be
resumed past kept nodes with pending dependents.

## Validation

F3.3 acceptance criteria with SIGKILL at barriers (no sleeps), including a
second interruption, and the human-wait scenario of ADR-0017 completing after a
process restart.
