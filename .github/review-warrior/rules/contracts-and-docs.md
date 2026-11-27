---
include:
  - "docs/features/**"
  - "docs/adr/**"
  - "docs/graph-spec.md"
  - "docs/domain-model.md"
  - "docs/architecture.md"
  - "internal/domain/graph/**"
  - "internal/domain/events/**"
  - "internal/adapter/yamlgraph/**"
---

# Contracts, specs and ADRs

- Code and its spec or ADR change together. Flag code that contradicts the spec it cites, and a spec or ADR change the code does not follow.
- Acceptance criteria must be measurable. Flag a criterion that no test can check.
- Graph semantics changes need an explicit `schema_version` path and must keep edges as data dependencies; an ordering-only edge is a finding.
- Graph files never embed secrets. Parsing and validation stay deterministic.
- A durable contract change without an ADR is a P2; with a broken reader of existing data it is a P1.
