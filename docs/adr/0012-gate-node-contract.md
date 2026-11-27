# ADR-0012 - Deterministic Gate node and restricted comparison command

**Status:** accepted for F1.4 implementation
**Date:** 2026-10-19
**Amended by:** ADR-0042 (proposed): the Gate kind `test_command` runs an author-chosen command inside a loop only; the standalone Gate and the `command` kind stay read-only as decided here.

## Context

F1.4 needs explicit deterministic graph acceptance without writable arbitrary
check commands. Agent evidence is success-only and cannot represent a red
Gate. F1.5 will separately normalize full CheckResult scope/duration/references.

## Decision

One check per type:gate node. Required nonempty check_id and kind select
command, json_predicate or reported_file_scope. Green emits a check fact,
succeeds and exposes only declared verdict:string = green. Red emits the fact,
fails and exposes no accepted outputs; existing fail-fast skips dependents.
Operational/config/input/cancellation errors are distinct from valid red and
never invent a verdict. Existing independent running peers retain semantics.

### Schema-v0 authoring

Command config: check_id, kind:command, canonical absolute cwd,
command:/usr/bin/diff, left_input and right_input naming required string ports
whose bound values are relative file paths. Reject graph args, environment
injection, alternate executable/options, shells/scripts/test runners.

Predicate config: check_id, kind:json_predicate, input naming a required JSON
port, nonempty field list of literal object keys, operator:eq|ne, required
expected scalar or null, or mutually exclusive expected_json containing an exact
JSON scalar/null literal string. Float shorthand is rejected with guidance to
expected_json: opaque YAML config decoding may already round it. Use
expected_json for decimal/exponent and integers beyond the YAML native range;
generic graph parsing and semantic hashing remain unchanged. No wildcard/array traversal/expression/coercion.
Missing field or mismatch is red; malformed input/config is error. Numbers
compare exact mathematical values (including large integers) without float64
rounding; 1 equals 1.0, number never equals string.

Reported scope config: check_id, kind:reported_file_scope, input naming
required JSON array port, allowed_paths list. Accept Agent-compatible
{path,provenance} records but label all Gate evidence reported, regardless of
upstream label. Portable relative paths reject absolute, NUL, backslash and
parent traversal segments; normalize dot/repeated separators. Allowed paths
are exact files or trailing-slash directory prefixes with segment boundaries.
Sort/deduplicate violations; malformed records error, valid out-of-scope red.
No Git verification/enforcement is performed by this check.

Only verdict:string may be declared as Gate output. Referenced input ports
must exist with matching required types; config rejects unknown fields before
execution. Ordinary data edges bind Agent outputs and Gate verdict; no hidden
state or ordering-only dependencies. Generic graph validator remains generic.

### Read-only command boundary

Support Darwin/Linux fixed trusted OS /usr/bin/diff, generated argv only.
Compare two regular non-symlink confined files under explicit cwd, preferably
preopened read-only FDs via ExtraFiles and /dev/fd/3,/dev/fd/4. Verify descriptor
behavior in supported platform checks; unsupported behavior fails closed.
Controlled environment is only LC_ALL=C, never ambient PATH/LD_*/DYLD_* or
caller environment. Exit0 green, exit1 red, >1/signal/start/drain errors are
operational failures. Use context cancellation, wait/reap and bounded1s pipe
drain. Trusted diff does not spawn arbitrary work or write comparison files.

This restricts the supported operation; it is not a universal process sandbox.
OS binary integrity is trusted. Path preflight is not a race-proof filesystem
sandbox; no concurrent hostile workspace mutation/isolation is promised.
Never broaden to arbitrary commands when restrictions are unsupported.

Capture at most8192 bytes per stdout/stderr stream AFTER streaming exact-value
redaction, while draining/counting omitted safe bytes. Reuse/extract only the
necessary existing redaction utility or supply runtime redaction context to
the deterministic Gate; never forward secret values into Agent/model input.
Raw process evidence stays byte-safe (base64 if needed); exposed string fields
must satisfy the existing observed UTF8/redaction preflight contract. Do not
reuse Shell's unbounded capture. Evidence is bounded and reports truncation.

### Minimal check trace

Add schema4 gate_check with check_id, kind, verdict, finite short reason and
bounded typed evidence. Command evidence includes optional exit code and
bounded stdout/stderr plus omitted-byte counts. Scope evidence explicitly
source:reported. At most32 evidence items and32768 total encoded payload bytes;
represent excess evidence with a count/marker, never unbounded raw values.
F1.5 owns complete duration/general scope/artifact-reference result schema.

Add narrow typed check observation. Worker buffers at most one fact;
coordinator validates consistency, detaches/redacts/bounds it and commits on
valid green/red before terminal lifecycle. Invalid, duplicate or inconsistent
observation becomes execution error with no accepted data and no panic.
Gate facts are retained independently of droppable progress. Run/node identity
is scheduler-owned; attempts remain absent. Red is failed Gate plus fact,
never accepted red graph output. Operational errors may retain bounded local
process evidence without claiming a red check.

## Alternatives considered

- Arbitrary commands called read-only: rejected; writable test runners cannot
  satisfy the invariant without real OS containment.
- Platform sandbox framework: unnecessary dependency/portability cost for the
  initial narrow operation; later execution policy can extend this explicitly.
- Success-only Agent evidence: cannot represent red acceptance decisions.
- Full CheckResult now: reserved to F1.5; minimal typed verdict fact suffices.

## Consequences

Graphs can accept/reject explicit data and file comparisons deterministically.
Command support is intentionally narrow. Scope remains advisory reported data,
and no isolation/retry/model judgment or dependency is added.

## Compatibility and migration

Graph schema0 and existing Shell/Agent interfaces remain valid. Emit event
schema4; decode existing schemas1–4 and reject gate_check under earlier versions.
CLI envelope stays1. F1.5 must migrate minimal Gate facts without changing
verdict/downstream semantics; keep old event readers as documented.

## Validation

Real fixed diff0/1, bounded/truncated/redacted output, forbidden writes/path
forms, cancellation, predicate exact values and reported-scope tests. Runtime
proves red blocks downstream, green releases once, correlation/retention and
safe invalid observations. CLI fake Agent→Gate→Shell uses real bindings.
Run targeted suites, make check and full race suite before independent review.
