# ADR-0015 - Static units, Return Envelopes and immutable acceptance

**Status:** accepted for F1.7 implementation
**Date:** 2026-10-19

## Context

F1.6 already retries a failed node while withholding candidates. F1.7 makes
unit identity/feedback/accepted provenance explicit. Dynamic multi-item
producer fanout and filesystem isolation remain outside this feature.

## Decision

### Explicit static batch

Add optional top-level units:[node_id,...] to schema0. It is an unordered set
of loop-enabled Agent node references; unit_id equals node_id within a run.
Enrolled unit/node IDs are bounded to256 UTF-8 bytes, matching existing check
identity safety; legacy/unenrolled IDs retain their existing contract.
Reject present-null/empty, duplicate, missing/unknown/non-Agent/nonloop refs
before adapter detection. Sort membership for semantic hashing; list order
never implies execution order. Absent units preserves F1.6 graphs/hashes and
large authored-scope behavior.

A static batch is authored sibling nodes, one logical retryable item per node.
Existing NODE.outputs.PORT bindings select accepted values. Multi-item
producers must be split into nodes to gain independent retry; no opaque batch
producer protocol, fanout, ordering edge or replanning transition is added.
Envelopes/acceptance registry apply only to enrolled units.

### Scope and size

Enrolled unit scope comes from explicit original allowed_paths, not checked
Gate ports/model claims. Normalize portable relative paths and directory
prefixes; reject absolute/NUL/backslash/parent traversal. Root ./ represents
the whole workspace. Reject invalid/oversized scope before execution rather
than silently truncate an apparently complete envelope. Maximum32 entries,
512 UTF8 bytes each,8192 encoded JSON bytes for the complete represented scope.
Validate after configured redaction too. A scope_redacted flag distinguishes
redacted opaque path text from normalized literal paths; no contents are
silently omitted. Existing non-enrolled Agent input contract stays unchanged.

### ReturnEnvelope v1

Typed payload includes return_envelope_version:1, unit_id, verdict, reason,
evidence, intended_scope, scope_redacted, attempt_id, linked check_id, and source
run_id/node_id/node_run_id/graph_version. Source IDs plus AttemptID and CheckID
identify the exact CheckResult. UnitID equals source NodeID.

Build only from retained sanitized validated CheckResult. Reason/evidence/
verdict must equal the linked result; no raw provider metadata or second
reinterpretation. Scope is separately normalized/redacted original intent.
Strict exact fields/version/types/correlation, positive-known identities,
scope and existing evidence rules apply. Envelope encoded maximum65536 bytes,
no new raw output; deep-copy all pointers/slices/references. Runtime validates
source/attempt/result against its current unit state, not only DTO shape.

### Immutable acceptance

Coordinator-owned registry keyed by unit ID records registration/current
attempt and accepts once. Reject missing/unknown/duplicate unit, wrong
source/attempt, contradictory envelope/result and ALL second publication,
including red overwrite. Green alone freezes accepted runtime values with
provenance (RunID,NodeRunID,accepted AttemptID,port); no invented per-port IDs.
Expose accepted AttemptID/provenance as needed; reads return detached copies.
Only rejected units get another attempt under F1.6 budget/error/cancel policy.
Exhaustion keeps accepted siblings, skips failed dependents and yields existing
needs_intervention/CLI4.

### Shared checkout scheduling

In unit-mode consider ALL Agent cycles, even unenrolled ones. Producer plus
embedded checking is one protected cycle. Prove distinct physical workspace/
Git-worktree identity before concurrent Agent cycles; canonical equal/nested
roots, sibling directories in one checkout, symlink aliases or unresolved
identity serialize. Distinct Git worktrees remain distinct despite common Git
directory, unless their writable roots physically nest. Scope disjointness
never authorizes parallel Agent cycles in one checkout. Preserve stable node
order/batch barrier and parallelism for proven distinct workspaces; Shell
semantics are unchanged.

Use bounded read-only identity inspection with controlled environment/no
hooks/fsmonitor, no implicit process/environment permission widening. Unknown
identity is conservative, not an execution grant. Record declared overlap or
unknown-scope/identity potential conflicts as structured diagnostics, explicitly
not actual Git-change proof. Bound conflict details to64 records with an
omitted-conflict count; retain diagnostic facts independent of progress.
Scheduling is only in-run: no protection from Shell/external writers, hostile
concurrent path mutation, scope violation, rollback or filesystem isolation.

### Trace and compatibility

Emit schema7 return_envelope and declared-scope-conflict facts, retain outside
progress limits, deeply copied. Checked attempt order:
CheckResult -> ReturnEnvelope -> attempt_finished; green acceptance committed
once. Operational failures invent neither envelope nor verdict. Invalid
state/input yields safe actionable diagnostics, never append-invariant panic.

Read schemas1–6 unchanged; preserve schema5/6/7 typed CheckResult retrieval and
attempt lookup. CLI envelope1 remains. Existing graph outputs select only
accepted values. No ReturnEnvelope for absent-unit legacy loops.

## Alternatives considered

Automatically enrolling all loops would break F1.6's large-scope contract.
Dynamic batch fanout is unnecessary to prove scoped retry. Trusting advisory
disjoint paths for parallel writes is unsafe. Global serialization of proven
distinct workspaces wastes valid concurrency; unresolved identities serialize.

## Consequences

Accepted runtime outputs/provenance are immutable while C is corrected.
Shared files may still change, including accepted sibling files; tests/docs
must demonstrate that limitation rather than claim isolation. No F1.8 Git
enforcement, F3.1 worktree isolation, general planner or dependency is added.

## Compatibility and migration

units is explicit opt-in; nil membership preserves old hashes/semantics.
New result consumers support schema7; old events remain readable without
invented unit/envelope records. IDs derive from existing graph nodes.

## Validation

Barrier A/B/C/D counts1/1/2/1 with A accepted before C red; accepted IDs/values
unchanged, green-only downstream once, exhaustion retains siblings. Strict
envelope/correlation/scope/duplicate/overwrite/deep-copy tests. Unit opt-in and
hash compatibility, same/nested/alias/unresolved/mixed-writer serialization,
distinct-worktree overlap proof and declared-conflict limits. Shared-repo
side-effect test demonstrates no filesystem immutability. CLI fake-Codex
counts/trace, full make check/race and fresh review precede merge. After merge,
real installed-Codex demonstration proves bad→red→correct within3 and only
failed unit retry, with inspectable trace and accepted provenance.
