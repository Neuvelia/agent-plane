# ADR-0002 - Graph reference and validation diagnostic contract

**Status:** accepted
**Date:** 2026-09-26

## Context

F0.1 stores graph bindings as opaque strings. F0.2 must validate their
referential integrity deterministically and return findings that future CLI and
UI consumers can render without interpreting free-form errors. Dot-delimited
references are ambiguous if identifiers may contain dots.

## Decision

- Graph identifiers and port names use lowercase ASCII identifiers beginning
  with a letter and continuing with letters, digits, hyphens, or underscores.
- Dot-delimited references are limited to `NODE.outputs.PORT`,
  `NODE.inputs.PORT`, and `inputs.INPUT`.
- `graph.Validate` returns a complete `ValidationResult`, including all
  independent practical findings, rather than failing at the first semantic
  error.
- Each diagnostic has severity, stable code, message, graph ID, definition
  location, and an optional suggestion. Locations use stable graph-definition
  paths and optional edge or graph-input-binding indexes. YAML positions are
  not invented because the F0.1 parser does not preserve them.
- Error diagnostics block validity; warning diagnostics do not.

## Alternatives considered

### Permit dots in identifiers

Pros:

- More permissive authoring names.

Cons:

- Makes the current binding syntax ambiguous without adding escaping or a new
  reference representation.

### Return only a Go error

Pros:

- Smaller initial API.

Cons:

- Loses independent findings and forces future CLI/UI consumers to parse
  human-oriented text.

## Consequences

### Positive

- References can be parsed and validated without an expression language.
- Diagnostics are deterministic and usable by both a future CLI and UI.
- Validation remains pure and performs no execution or external I/O.

### Negative / trade-offs

- Existing syntactically parseable F0.1 definitions using other identifier
  characters become semantically invalid under F0.2.
- YAML source locations require a later parser-contract extension if needed.

## Compatibility and migration

F0.1 intentionally deferred semantic reference validation, so no previously
valid execution contract changes. Authors must update references that use
identifiers outside the documented grammar before their graphs can validate.

## Validation

Unit tests cover valid references, missing nodes and ports, incompatible types,
required inputs, cycles, warning-only lint, multiple diagnostics, and stable
results. The repository gate is `make check`.
