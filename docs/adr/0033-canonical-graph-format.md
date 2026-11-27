# ADR-0033 - Canonical graph format, graph version and JSON input

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-26
**Source:** roadmap audit, decisions D5 and D15

## Context

Roadmap F0.1/F0.3 require explicit edges with output→input bindings (no bare
arrows), a `schema_version` field, fields `graph id, version, inputs, nodes,
edges, outputs, metadata`, and JSON as an equivalent representation. The
architecture plan §11 example uses `version: 1`, `name`, `depends_on`,
`foreach`, `{{ }}` templating and `when:`, with no `edges` section. The repo
implements the roadmap variant (`docs/graph-spec.md`, strict unknown-field
rejection) and computes a semantic hash `graph_version`; it has no
author-written version field. JSON files parse only because YAML is a superset
of JSON.

## Decision

1. The repo format in `docs/graph-spec.md` is canonical. The architecture §11
   example is an outdated sketch; `depends_on`, `foreach`, `when` and `{{ }}`
   interpolation are not part of schema v0.
2. The roadmap's graph "version" is the computed semantic `graph_version`
   (hash). There is no author-written version field. A future registry
   (F5.7) may add release versions through its own ADR.
3. JSON graph files are supported through the YAML parser. A contract test must
   prove a JSON file and the equivalent YAML file yield the same definition
   and `graph_version` (issue #179).

## Alternatives considered

### Add an optional author `version` field

Pros: human-readable versions. Cons: a second version notion next to the hash,
a schema change, no consumer before F5.7.

### Drop the JSON requirement

Pros: less to test. Cons: departs from the roadmap without need; the parser
already accepts JSON.

## Consequences

- F0.1 keeps one open gap (JSON contract test) and one for unknown node types
  (#178).

## Compatibility and migration

None.

## Validation

Issue #179 adds the JSON contract test; the registry tracks it as F0.1 work.
