# ADR-0033 - JSON field selection in data bindings

**Status:** proposed
**Date:** 2026-11-26
**Feature:** F2.3b (`docs/features/F2.3b-json-field-selection.md`); amends the F2.3 non-goal on transformations
**Closes:** issue #185 (owner decision Q17: a gap until an ADR decides)

## Context

Roadmap F2.3 scope is "simple 1:1 mapping, plus selecting a field from a JSON object". 1:1 mapping is
implemented. Selecting a field is not: F2.3 lists "transformations on an edge, expressions" as a non-goal and
sends complex transformation to a future Code node, but that wording does not name the roadmap item, and graph
schema v0 has no selector.

Facts that bind the decision (`docs/graph-spec.md`, ADR-0003):

- Each edge moves exactly one value from a declared output to a declared input.
- Declared port types and runtime value types must match exactly; no implicit coercion, including between JSON
  and scalar types. Types: five declared types including `json`.
- Binding references are typed strings; `schema_version: 0` is the only accepted version and the semantic hash
  (F0.6) covers edges, so a new field changes graph semantics and must be hashed.
- No code node exists in the roadmap catalog today (only mentioned as future in the reference docs).
- Data dependencies are the contract (AGENTS.md rule 4); no hidden dependencies.

The decision therefore changes graph semantics and likely the schema version: an ADR trigger.

## Decision (recommended; the owner chooses)

Recommended option **B**: a minimal, declarative **JSON Pointer selector** on bindings, introduced as graph
schema v1 and gated by the version; expressions and transformations stay out of scope.

1. An edge and a graph-input binding may carry an optional `select: "<RFC 6901 JSON Pointer>"` (for example
   `/result/count`). Absent means today's 1:1 behavior.
2. The source port must be declared `json`. The target input must declare one of `string`, `number`, `boolean`,
   `json` (not `artifact_ref`). The selected value must match the target's declared type exactly (no coercion).
3. Static validation checks pointer grammar and the type pairing. Existence of the pointer cannot be known
   statically; a missing path or type mismatch at run time fails the **consumer** with a typed binding error
   naming edge, pointer and observed JSON type, before the consumer executes. It is never silently null.
4. The schema version becomes 1 only for graphs that use `select`; a graph that does not use it stays valid as
   v0 and keeps a byte-identical semantic hash. The reader accepts 0 and 1. Writers emit 1 only when needed.
5. The F2.3 Bindings panel offers a selector field only for `json` sources, validated by an engine operation
   (`set_edge_select`, additive to the ADR-0020 vocabulary) so no pointer logic lives in TypeScript.

## Alternatives considered

### A. Leave it to a future Code node (status quo, now named)

Pros: no schema change; keeps bindings trivially analyzable; matches the F2.3 non-goal wording.

Cons: no Code node exists or is scheduled; every JSON field extraction needs a Shell node with `jq` (a Shell
node, with command and environment concerns) for what the roadmap calls a simple mapping; the roadmap item stays
unmet.

### B. JSON Pointer `select` on bindings, schema v1 (recommended)

Pros: closes the roadmap sentence with the smallest possible mechanism; RFC 6901 is a standard with no
expression evaluation, so no new language; exact-type rule is preserved; hash and diff behavior stay clean.

Cons: schema version bump and a new reader branch; run-time-only existence errors; one more thing the edge
inspector and recorded values (ADR-0023) must explain (the recorded value is the selected value, with the pointer
shown).

### C. Dedicated built-in `select` node type

Pros: no binding change; a node is a visible step on the canvas.

Cons: adds a node type (descriptors, executor, canvas, events) for a one-line extraction; a node per field
clutters graphs; still changes the schema (new node type).

### D. Expression language in bindings (JSONPath, jq, CEL)

Pros: powerful.

Cons: an evaluation language, security and determinism surface, hard to type statically; contradicts the F2.3
non-goal. Rejected.

## Consequences

### Positive

- The roadmap F2.3 scope is met without a general transformation language.
- Static type checking remains exact; failures are typed and attributable.

### Negative / trade-offs

- First schema version bump since v0: parser, validator, hash, serializer, every reader of definitions and the
  desktop must handle two versions.
- A pointer can fail only at run time; the editor can only preview against recorded values.

## Compatibility and migration

- v0 graphs are unchanged in meaning and hash. A v0 reader (older build) rejects a v1 graph with the existing
  unsupported-version error; this is the intended gate.
- Events: edge values continue to carry the selected value; `schema_version`-independent. No event schema number
  is consumed by this ADR; if the binding error needs a new typed event reason it would be additive within the
  schema number owned by the wave that lands it (coordinate with the owner of the next event schema bump).
- Run store and recorded values (ADR-0023): the value recorded for a selecting edge is the selected value; the
  pointer is read from the graph.

## Validation

F2.3b acceptance criteria: validator and hash tests, a run that selects a nested number, missing-path and
wrong-type failures, v0 byte-identical behavior. Not implemented by this ADR.
