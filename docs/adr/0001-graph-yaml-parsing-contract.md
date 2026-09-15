# ADR-0001 - Graph YAML parsing contract

**Status:** accepted
**Date:** 2026-09-15

## Context

Graph definitions are a durable, Git-stored execution contract and are treated
as untrusted input. F0.1 needs a YAML implementation, an unknown-field policy,
and extension points for descriptive metadata and node-type-specific
configuration. These choices affect every future graph and parser consumer.

The Go standard library does not provide YAML support. The maintained YAML
organization publishes stable v3 releases and a v4 line that is still a
release candidate at the time of this decision.

## Decision

- Use `go.yaml.in/yaml/v3` as the single runtime dependency for YAML parsing and
  serialization, pinned by Go modules.
- Accept exactly one YAML document per graph definition.
- Require `schema_version` to be a YAML integer. Reject non-integer scalar
  forms and reject every integer other than `0` with typed, inspectable
  errors.
- Decode schema v0 with `Decoder.KnownFields(true)` so unknown fields fail at
  typed schema boundaries.
- Allow arbitrary keys only within `metadata` and node `config`, the two
  explicit extension mappings.
- Keep YAML-library types out of AgentPlane's exported graph package API.
- Preserve edge, graph-input-binding, and graph-output references as named
  strings. F0.1 does not parse or validate reference semantics.
- Treat configuration scalars as inert data. Parsing performs no command or
  template execution, environment expansion, secret resolution, filesystem
  access, or network access.

## Alternatives considered

### `go.yaml.in/yaml/v4`

Pros:

- It is the upstream project's active development line.
- It offers structured load errors, configurable options, and resource-limit
  plugins.

Cons:

- Only release-candidate versions are available at the time of this decision.
- Adopting it would expose the initial public graph contract to pre-release API
  and formatting changes.

### Tolerate unknown fields

Pros:

- Newer producers could add fields without immediately breaking older
  consumers.

Cons:

- Misspelled execution fields could be silently ignored.
- A graph could parse successfully while expressing behavior different from
  what its author intended.

### Model every node configuration as a closed union

Pros:

- Every configuration field would be statically typed at the graph boundary.

Cons:

- F0.1 would need to define future node implementations prematurely.
- Adding a node type would require editing the base graph serialization union.

## Consequences

### Positive

- Authoring mistakes at stable schema boundaries fail early and
  deterministically.
- Metadata and node configuration can evolve without weakening the core graph
  contract.
- The parser API remains independent of the YAML implementation and can move
  to a stable v4 release later.
- Later semantic validation receives typed definitions and typed reference
  categories without F0.1 implementing F0.2 behavior.

### Negative / trade-offs

- v3 receives security fixes but not routine feature development.
- Diagnostic positions are primarily provided through wrapped YAML errors
  rather than an AgentPlane-specific source-location model.
- Strict schema evolution requires an explicit versioning or compatibility
  decision.
- The parser cannot determine whether an arbitrary opaque configuration value
  contains a secret.
- F0.1 does not add configurable YAML depth or alias limits.

## Compatibility and migration

Schema v0 is the first implemented graph contract, so no existing serialized
contract requires migration. The checked-in example graph conforms to v0.

A future move to YAML v4 must retain the `graph.Parse` and `graph.Marshal`
behavioral contracts, rerun round-trip and error tests, and document any
intentional diagnostic or formatting differences. YAML types must not leak
into consumers during that migration.

## Validation

Unit tests cover the minimal schema, the checked-in parallel example, all
declared value types, graph input bindings, strict unknown fields, missing and
unsupported versions, malformed and multi-document YAML, opaque extension
maps, inert strings, syntax-only semantic errors, serialization, and semantic
round trips. The repository gate is `make check`.
