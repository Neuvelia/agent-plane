# ADR-0003 - Typed data binding resolution contract

**Status:** accepted
**Date:** 2026-10-07

## Context

F0.1 defines explicit graph-input bindings and node-output edges, and F0.2
validates their references. F0.3 needs to transfer actual values without
changing schema v0 or introducing scheduling, transformations, or shared
context. The value and missing-data rules will be consumed by later runtime
features, so they form a durable contract.

## Decision

- Keep `EdgeDefinition` and `GraphInputBindingDefinition` as the only YAML
  binding representations. Compile validated references into an immutable
  `BindingPlan`; do not add a second authoring syntax.
- Store runtime values in a tagged `Value` with private payload fields and
  typed constructors/accessors. Numbers retain JSON decimal text; JSON bytes
  are validated and copied; artifact references are opaque nonempty handles.
- Match declared and supplied types exactly. Missing required graph inputs,
  missing bound node outputs, invalid supplied names, and runtime type
  mismatches return stable structured error diagnostics. No partial node input
  map is usable when resolution has errors.
- Allow a missing optional graph input to leave an optional target unset, but
  never let a required graph input or required target remain unsatisfied. A
  bound node output is required even if its target is optional. JSON `null`
  is present data, not a missing value.
- Derive each node's direct dependencies from incoming node-output edges only.
  Graph input bindings do not create node-to-node dependencies. F0.3 does not
  track completion or schedule nodes; callers resolve a node after its source
  nodes have succeeded.

## Alternatives considered

### Add a new binding object to schema v0

Pros:

- Could make parsed endpoint fields directly visible in YAML types.

Cons:

- Duplicates an already explicit edge and graph-input-binding contract.
- Forces a migration without adding required F0.3 behavior.

### Use `map[string]any` or implicit JSON coercion for runtime values

Pros:

- Fewer initial value constructors.

Cons:

- Weakens port type guarantees and can lose number precision or make
  resolution depend on ad hoc conversions.
- Leaves mutable structured values available to multiple consumers.

## Consequences

### Positive

- Existing graph files and F0.1/F0.2 behavior remain compatible.
- The same declared graph and values resolve deterministically.
- Later schedulers can inspect direct dependencies without inferring them
  from execution order.
- Binding resolution performs no external I/O or model calls.

### Negative / trade-offs

- A later executor must provide typed values and call graph-input validation
  before starting a run.
- A missing node output is terminal only after its producer has completed;
  F0.4 must enforce that precondition.
- Artifact handle existence and graph-output materialization are not checked
  by this layer.

## Compatibility and migration

Schema version remains `0`. Existing YAML fixtures require no migration.
The new Go API is additive. Static errors still come from F0.2's validator;
runtime value errors use its diagnostic envelope with distinct codes.

## Validation

Unit tests cover graph-input fan-out, node-output transfer, dependency
derivation, missing and mistyped values, JSON and artifact pass-through,
determinism, validation integration, and existing F0.1/F0.2 regressions.
The repository gate is `make check`.
