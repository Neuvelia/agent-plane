# Contributing

## Principle

Every non-trivial code change should start from a written, testable contract.

For roadmap work, that contract is a file under `docs/features/`.

## Feature workflow

1. Pick exactly one active feature.
2. Read `AGENTS.md` and the feature spec.
3. Create a branch/worktree.
4. Ask Codex for a plan using `prompts/plan.md`.
5. Review the plan before implementation.
6. Implement only the approved scope.
7. Run `make check`.
8. Review the diff using a separate session and `prompts/review.md`.
9. Open a PR using the repository template.

## Branch names

Use the feature identifier where possible:

```text
codex/F0.1-graph-schema
codex/F0.2-graph-validator
fix/event-sequence-regression
```

## Commit messages

Prefer conventional, narrow messages:

```text
feat(graph): add schema v0 types
test(graph): reject missing node references
fix(runtime): preserve accepted sibling output on failure
docs(adr): record event store decision
```

## Pull requests

A PR should contain:

- one coherent feature or fix;
- the relevant tests;
- updated docs if a contract changed;
- an ADR for durable architecture decisions;
- actual check results;
- explicit known limitations.

Avoid unrelated cleanup in feature PRs.
