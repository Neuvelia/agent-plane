# Implementation queue

The ordering below is intentional. Complete one vertical contract before moving to the next.

## Phase 0 - Execution Core (implemented)

- [x] F0.1 Graph Definition Schema v0
- [x] F0.2 Graph Validator and lint
- [x] F0.3 Explicit data bindings
- [x] F0.4 In-memory DAG scheduler
- [x] F0.5 Shell Node
- [x] F0.6 Event model and run/node IDs
- [x] F0.7 CLI runner

The CLI integration test proves independent branch overlap, bound values in a
dependent Shell node, ordered events, and a deterministic terminal result.

## Phase 1 - Agent Loop

- [ ] F1.1 Agent Adapter Contract (spec, ADR and plan ready; code pending)
- [ ] F1.2 First coding-agent adapter
- [ ] F1.3 Unified Streaming
- [ ] F1.4 Gate Node
- [ ] F1.5 Check Result Contract
- [ ] F1.6 Correction Loop + maxAttempts
- [ ] F1.7 Return Envelope / scoped retry
- [ ] F1.8 Changed-files Scope Check (P1)

Review each feature spec before implementing it. F1.1 is the first active
implementation task; F1.2-F1.7 specs are drafts for review. See
`docs/roadmap.md` for dependencies and phase exit criteria.

## Phase 0 milestone demo (achieved)

The phase is complete when an example graph equivalent to:

```text
prepare
 /   \
A     B
 \   /
 merge
```

can execute A and B concurrently, wait for both before merge, pass explicit outputs, emit structured events, and return a deterministic CLI exit code.
