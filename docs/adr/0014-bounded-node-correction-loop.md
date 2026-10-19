# ADR-0014 - Bounded node-local correction attempts

**Status:** accepted
**Date:** 2026-10-19

## Context

F1.5 supplies bounded typed Gate feedback. F1.6 retries one logical unit
without exposing rejected candidates, adding DAG cycles or provider-specific
scheduler behavior. Envelopes and filesystem isolation remain later work.

## Decision

Add optional typed node-level loop outside config in schema0. Absent loop
preserves execution and semantic hashes. max_attempts defaults3; explicit
integer1–10 includes initial produce. Reject zero/negative/>10/null/noninteger.
Exactly one embedded Gate has inputs/config, implicit type:gate, no recursive
loop/own DAG node/exported output. Bind only candidate.outputs.PORT or
node.inputs.PORT -> gate.inputs.PORT; require a candidate-output dependency,
declared matching types, unique targets and complete required inputs.
External data first enters ordinary node inputs. No ordering-only dependency.

Initial loop producer is Agent with explicit allowed_paths (empty means no
intended changes, still advisory). Validate ALL loop syntax/config/bindings
before capability detection. A narrow optional executor preflight seam lets
caller composition validate Agent/Gate contracts; affirmative optional
correction-feedback capability is required before provider execution. Unknown
or unsupported fails preflight. Nonloop executors/adapters remain valid.

Coordinator creates unique AttemptIDs under stable run/node-run/version,
owns transitions and publication; workers execute one produce/check cycle.
Validate producer outputs before Gate binding; keep candidates outside accepted
maps. Green accepts once. Only valid red retries that node; completed batch
peers never rerun and existing logical batch barrier remains. Provider/process/
config/output/Gate operational/invalid-observation errors are fatal, no retry.
Exhaustion keeps failed node/run state with needs_intervention failure kind,
safe code, skipped dependents and CLI4. Cancellation checked before dispatch,
check and correction preserves its cause and prevents later calls; unrelated
racing errors remain errors.

AgentExecutionInput gains optional typed feedback: previous AttemptID, previous
sanitized CheckResult and original intended scope. Original Task is unchanged.
Validate/detach/redact/bound result once, retain it and pass an equal detached
copy next time. Checked-input scope is not correction scope. Codex transports
explicit untrusted stdin feedback without changing process/env/permissions.
Capability means transport support, not model obedience.

Emit schema6 attempt_started/attempt_input/attempt_finished and gate_check,
with outcomes accepted/rejected/needs_intervention/execution_error/cancelled.
Attempt facts/checks survive progress limits. Logs/progress/evidence correlate
current AttemptID; close/flush per-invocation observers, discard late callbacks.
Input trace is a bounded observable snapshot: task fingerprint, explicit
redacted intended scope/safe reference metadata, presence/omission flags and
previous-result linkage. No raw task/environment/provider metadata/reasoning
or replay guarantee. Validate inconsistent observations fail closed.

Attempt-correlated node_evidence records successful PRODUCER observation,
including rejected candidates; accepted attempt outcome controls publication.
Without AttemptID old success-only node evidence retains meaning.

Decode1–5 unchanged; schema5 CheckResults remain retrievable after bump6,
with additive detached attempt-filtered lookup. Graph schema0 stays additive
with nil-loop hashes unchanged; CLI envelope1. Nonloop attempts stay absent.

## Alternatives considered

DAG back edges violate local loops; retrying every error hides broken
configuration/processes; adapter-owned IDs/publication undermine evidence;
raw input trace adds secret exposure and an unnecessary replay promise.

## Consequences

Default3/max10 corrections with accepted values withheld until green.
Filesystem edits survive rejected attempts; no rollback/isolation/enforcement.
Git evidence is per invocation, not cumulative certification. No F1.7 envelope,
batch authoring, replanning, dependency or persistence is added.

## Compatibility and migration

Old nonloop behavior stays. Loop graphs require feedback-capable composition.
Schema6 consumers distinguish producer evidence from accepted output and read
old schema5 checks without invented attempts.

## Validation

Budget/preflight/fatal-error tables, red→green unique IDs and downstream once,
equal immutable feedback, barrier cancellation at all boundaries, retained
input/check/outcomes, redaction/late callbacks, old schema/hash/lookup and CLI
intervention tests. Targeted suites, make check and race before fresh review.
