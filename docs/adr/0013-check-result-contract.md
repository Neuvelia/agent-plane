# ADR-0013 - Versioned Check Result and typed trace retrieval

**Status:** accepted for F1.5 implementation
**Date:** 2026-10-19

## Context

F1.4 already handles verdicts, bounded byte evidence, observation validation,
redaction and downstream blocking. F1.5 stabilizes scope, duration and evidence
references without changing those deterministic execution decisions.

## Decision

### Full result version1

Define typed CheckResult with check_result_version:1, check_id, kind, verdict
(green/red), short reason, evidence[], scope and duration_ns. Scope is an
explicit object naming actual checked input port names (sorted/deduplicated,
nonempty UTF-8 strings, at most32 inputs/256 bytes per name). It is checked-input
scope, not inferred filesystem permission/correction scope. Any predicate
selectors or reported violations belong in evidence, not correction scope.
Existing finite predicate reasons with typed evidence kind/source suffice; this
iteration adds no selector payload.
F1.6 supplies original intended correction scope separately.

Duration is required nonnegative integer nanoseconds; zero is valid, omitted
or null invalid. Measure Gate evaluation once with Go monotonic elapsed time.
Do not fabricate missing duration/scope for legacy records. A red result must
have nonempty reason and evidence; green may carry an empty evidence list
(command verdicts still require their exit-code evidence). All current Gate
producers retain F1.4 evidence. Operational/config/process/cancellation
errors never manufacture a CheckResult. Green cannot hide an execution error.

Keep F1.4 check-kind/verdict consistency,32 evidence-item limit,32768 encoded
result-byte limit,8192 bytes per captured stdout/stderr, existing short-string
bounds and explicit omission counts. Budget actual encoded bytes (including
escaping/envelope); redaction occurs before serialization/retention.

### Evidence and references

Each item has typed kind and source; preserve command exit/capture/omission
evidence and reported provenance. Support optional typed reference metadata
for file with optional line, test, diff and opaque artifact handle, without
reading/resolving resources. Reference states distinguish known/provided,
unavailable and invalid; absence of the reference is distinct from all three.
Identifiers and diagnostic reasons are at most512 UTF-8 bytes each.
Known means a nonempty identifier was supplied, not independently verified
resource existence. Unavailable/invalid carry a bounded diagnostic reason.
No explicit empty known identifier, contradictory kind selectors, malformed
structure or nonpositive present line is accepted. Line belongs only to file
reference; optional pointers preserve absent vs zero (zero line is invalid).
The optional `reference` object uses `kind`, `state`, `identifier`, `line` and
`reason` fields. `known` requires identifier and forbids diagnostic reason;
`unavailable`/`invalid` require reason and forbid identifier/line. Forbidden
fields are rejected by presence, including explicitly present empty values. A missing
reference is absent; explicit null reference/line is malformed. Evidence kind
matches the check kind; source matches it too, except `reported_file_scope`
uses source `reported`.
An invalid reference state is useful diagnostic data, not permission to emit
an invalid result structure. Validate all new strings as UTF8 and within
budgets; redact all reference/scope text. No artifact contents or raw provider
metadata are embedded.

Full result, scope, evidence and reference objects require exact JSON field
names; case aliases are unknown fields.
Unknown JSON fields, unsupported result versions, missing/null required fields,
wrong types, negative duration, unsupported verdict/kind, invalid red results
and oversized evidence fail before emission. Field order has no meaning.
Preserve zero exit/duration and byte evidence through JSON roundtrip.

### Trace and migration

Emit event schema5, retaining gate_check as the event type with the full
result payload. Read schemas1–4 unchanged; schema4 gate_check decodes as a
distinct legacy minimal payload, never a fabricated current result. New full
results require schema5; reject incompatible versions/shapes. CLI envelope1
and graph schema0 remain. Legacy decoding retains original duration/scope
absence. F1.5 creates no attempts; correlation remains in event envelope with
run/node/node-run and optional future attempt ID.

Retain one worker observation and coordinator consistency/identity/output/error
validation. Validate identity/kind before redaction, then detach/redact/validate
full result before committing on green/red ahead of terminal lifecycle. Invalid
observations fail closed with no accepted values/no trace panic. Preserve
retention independent of progress. Deep-copy every pointer/slice/reference.

Provide narrow typed trace retrieval filtered by run ID and node-run ID in
sequence order, returning detached current CheckResults. Do not parse errors
or logs, and do not silently upgrade legacy facts. Document legacy inspection
separately. CLI JSON already carries events; avoid redundant result copies.

## Alternatives considered

- Replacing Gate execution: unnecessary risk; normalize existing outcomes.
- Inferring correction filesystem scope from checked ports: unjustified; the
  original Agent intended scope is a distinct later input.
- Inventing legacy zero durations: loses unknown/zero distinction.
- Resolving references now: artifact/filesystem lookup belongs elsewhere.

## Consequences

Consumers receive deterministic typed feedback with explicit unknown/reference
states and scope. Current results can support later loops without parsing
prose. Old minimal evidence remains readable without misrepresentation.

## Compatibility and migration

New emitted schema5 requires updated consumers; old facts remain readable.
Gate graph config/verdict/downstream/process behavior is unchanged. No new
dependency/framework/attempt/retry/artifact store is added.

## Validation

Both verdicts roundtrip, including zero duration/exit, shuffled JSON fields,
reference states/kinds and scope. Reject malformed/unknown/oversized results
before trace entry. Retrieve red/green by run/node identity with detached
copies. Extend redaction/encoded-budget boundary tests and existing Gate/CLI
integration. Run targeted suites, make check and full race before review.
