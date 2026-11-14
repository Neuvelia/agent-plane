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

F1.1 is implemented with a test-only mock and generic runtime dispatch; its
contract decisions are in ADR-0009. F1.2 adds explicit Codex CLI routing and
Git-derived evidence (ADR-0010), verified by a real disposable-repository smoke.
F1.3 adds bounded normalized progress (ADR-0011). F1.4 adds deterministic Gate checks (ADR-0012). F1.5 adds versioned Check Results (ADR-0013), F1.6 bounded correction
attempts (ADR-0014), and F1.7 static-unit envelopes/immutable runtime acceptance
(ADR-0015). Shared files remain mutable; the after-merge real-Codex demo is
prepared but has not been executed in this implementation. F1.8 adds opt-in
Git-observed changed-files scope checking (ADR-0016). See `docs/roadmap.md` for the complete ID catalog,
dependencies and phase exit criteria. A roadmap entry is not an executable
feature contract.

| Feature | Spec status |
| --- | --- |
| `F1.1-agent-adapter-contract.md` | Implemented; contract mock only |
| `F1.2-first-coding-agent-adapter.md` | Implemented; real Codex smoke verified |
| `F1.3-unified-streaming.md` | Implemented; ADR-0011 |
| `F1.4-gate-node.md` | Implemented; ADR-0012 |
| `F1.5-check-result-contract.md` | Implemented; ADR-0013 |
| `F1.6-correction-loop.md` | Implemented; ADR-0014 |
| `F1.7-return-envelope-scoped-retry.md` | Implemented; ADR-0015 |
| `F1.8-changed-files-scope-check.md` | Implemented; ADR-0016 |

## Phase 2 vertical slice - drafts awaiting acceptance

| Feature | Spec status |
| --- | --- |
| `F1.6a-shell-loop.md` | Implemented; ADR-0018 |
| `F2.10-human-node.md` | Implemented; ADR-0017 |
| `F2.9-run-store.md` | Implemented; ADR-0019 |
| `F2.5-live-map.md` | Draft; ADR-0019 (accepted) |
| `F2.5a-desktop-redesign.md` | Approved; R0-R2 merged, R3 implemented; presentation only, no ADR |

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
