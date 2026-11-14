# Historical implementation queue

This archived checklist records the Phase 0 and Phase 1 implementation sequence.
For current product priorities, use `docs/roadmap.md` and the active feature
specification. Root `TASKS.md` is now a local, Git-ignored worktree journal.

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

- [x] F1.1 Agent Adapter Contract
- [x] F1.2 First coding-agent adapter (Codex; real smoke verified)
- [x] F1.3 Unified Streaming (bounded progress; ADR-0011)
- [x] F1.4 Gate Node
- [x] F1.5 Check Result Contract
- [x] F1.6 Correction Loop + maxAttempts
- [x] F1.7 Return Envelope / scoped retry
- [x] F1.8 Changed-files Scope Check (P1; Git-observed warn/deny; ADR-0016)

Review each feature spec before implementing it. F1.1-F1.8 are implemented;
Phase 2 features require their own reviewed specs before activation.
F1.7 has a prepared after-merge real-Codex demo; execution evidence is pending. See
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
