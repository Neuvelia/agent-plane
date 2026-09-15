# Codex development workflow

## Objective

Use Codex as an implementer and reviewer inside a controlled engineering loop rather than as an unconstrained repository generator.

## Standard flow

```text
feature spec
    |
    v
Codex plan-only session
    |
    v
human plan review
    |
    v
isolated branch/worktree
    |
    v
Codex implementation session
    |
    v
deterministic checks
    |
    +---- fail ----> implementation correction
    |
    v
separate Codex review session
    |
    v
human diff review
    |
    v
merge
```

## Worktree setup

Example:

```bash
git switch main
git pull --ff-only
git worktree add ../agentplane-F0.1 -b codex/F0.1-graph-schema
cd ../agentplane-F0.1
```

Use one active feature per worktree when possible.

## Planning session

Use `prompts/plan.md`.

The planning session should not edit files.

The plan must identify:

- current state;
- exact scope;
- files to change;
- contracts affected;
- tests;
- risks;
- ambiguous requirements.

Reject plans that silently add future roadmap features.

## Implementation session

Use `prompts/implement.md` after approving the plan.

The implementer should:

- make the smallest coherent change;
- add tests first or alongside behavior;
- run targeted tests during development;
- finish with `make check`;
- inspect the final diff;
- report limitations.

## Deterministic gate

`make check` is the minimum repository gate.

A green check is necessary, not sufficient. Acceptance criteria still need human and reviewer inspection.

## Review session

Start a fresh Codex session where practical.

Use `prompts/review.md` and ask for review-only work.

The reviewer should focus on:

- deviations from the feature spec;
- missing acceptance criteria;
- state-machine mistakes;
- concurrency hazards;
- scope expansion;
- unsafe process/path handling;
- missing failure tests;
- unnecessary abstractions;
- accidental coupling to future cloud/UI concerns.

## Human review

Review in this order:

1. feature spec;
2. implementation plan;
3. diff stat;
4. public types/contracts;
5. tests;
6. implementation hotspots;
7. actual check output.

The goal is not to manually re-read every line of boilerplate. The goal is to verify contract, architecture, evidence, and risk.

## After merge

Update `experiments/run-log.csv` for thesis/development measurements when the work is relevant to the study.

If implementation changed a durable architecture choice, ensure an ADR exists.
