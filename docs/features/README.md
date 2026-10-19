# Feature specifications

A feature spec is the executable contract for one roadmap item.

Implementation should not begin for a non-trivial roadmap feature until its spec has measurable acceptance criteria.

## Phase 0 - implemented

- `F0.1-graph-definition-schema-v0.md`
- `F0.2-graph-validator-and-lint.md`
- `F0.3-data-bindings.md`
- `F0.4-dag-scheduler.md`
- `F0.5-shell-node.md`
- `F0.6-event-model.md`
- `F0.7-cli-runner.md`

## Phase 1 - Agent Loop

F1.1 Agent Adapter Contract is the active implementation candidate, with its
contract decisions in ADR-0009 and an approved plan under
`docs/superpowers/plans/`. F1.2-F1.7 are P0 features; F1.8 is P1. See
`docs/roadmap.md` and
`docs/reference/full-roadmap-features.md` for the ID catalog, dependencies, and
phase exit criteria. Add each implementation spec here when its feature is
activated; do not treat a roadmap entry as an executable feature contract.

- `F1.1-agent-adapter-contract.md` — ready for implementation; no F1 code yet.

## Later phases

F2-F6 remain roadmap scope. Their IDs and priorities are tracked in
`docs/roadmap.md`; implementation specs are created when a feature becomes
active.

## Rules

A feature spec should describe behavior, not prescribe unnecessary internal structure.

Acceptance criteria must be testable.

Prefer:

```text
CLI returns non-zero exit code for a cyclic graph and identifies the cycle.
```

Avoid:

```text
Validation should be robust and user-friendly.
```

Use `_template.md` for new features.
