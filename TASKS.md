# Initial implementation queue

The ordering below is intentional. Complete one vertical contract before moving to the next.

## Phase 0 - Execution Core

- [ ] F0.1 Graph Definition Schema v0
- [ ] F0.2 Graph Validator and lint
- [ ] F0.3 Explicit data bindings
- [ ] F0.4 In-memory DAG scheduler
- [ ] F0.5 Shell Node
- [ ] F0.6 Event model and run/node IDs
- [ ] F0.7 CLI runner

## Recommended first work sessions

### Session 1 - F0.1 plan

Use `prompts/plan.md` with:

```text
FEATURE_ID=F0.1
FEATURE_SPEC=docs/features/F0.1-graph-definition-schema-v0.md
```

Plan only. Do not modify files.

### Session 2 - F0.1 implementation

Implement the approved plan. Run `make check`.

### Session 3 - F0.1 review

Use `prompts/review.md` in a separate Codex session. Review against the spec and `AGENTS.md`.

### Session 4 - F0.2

Repeat the same flow for `docs/features/F0.2-graph-validator-and-lint.md`.

## Phase 0 milestone demo

The phase is complete when an example graph equivalent to:

```text
prepare
 /   \
A     B
 \   /
 merge
```

can execute A and B concurrently, wait for both before merge, pass explicit outputs, emit structured events, and return a deterministic CLI exit code.
