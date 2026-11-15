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

## Task continuity

For implementation work with more than a few steps, maintain a root
`TASKS.md` in that worktree. Keep it short: objective and branch; links to the
active spec and plan; a checklist of completed and remaining steps; the last
verification command and result; blockers; and the next concrete action.
Update it when a meaningful step finishes and before handing off or pausing.
When resuming, read the journal and compare it with `git status` and the current
branch before continuing. The journal is ignored by Git and stays local to the
worktree; it is not the feature spec, implementation plan, or PR description.
Do not record secrets, raw model output, or sensitive repository content.
Small one-step tasks do not need a journal.

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

## CI matrix

`.github/workflows/ci.yml` runs `scripts/check.sh` (the body of `make check`)
on every push and pull request, on three runners: `linux-x64`
(`ubuntu-latest`), `macos-arm64` (`macos-latest`) and `windows-x64`
(`windows-latest`). Linux and macOS are required and also run `make build`.
Windows is reported but non-blocking (`continue-on-error`, 20 minute cap)
because main does not pass there yet; the job checks out with LF line endings
so `gofmt -l` means the same on every OS. Remove `experimental` for Windows once
it is green. The desktop shell stays outside this gate (`make desktop-check`).

Known Windows failures, found by reading the code (follow-up work, not fixed):

- `internal/gate/executor_test.go:199` calls `syscall.Mkfifo`, so the
  `internal/gate` tests do not compile for Windows. `go vet` stops
  `scripts/check.sh` there, so until this is fixed the Windows job never
  reaches `go test` and the items below stay unreported in CI.
- Production code hard-codes Unix tool paths: `/usr/bin/git` in
  `internal/gitstate/snapshot.go:64` and `internal/runtime/workspace.go:45`;
  the command Gate requires `/usr/bin/diff` and `/dev/fd/3`
  (`internal/gate/config.go:62`, `internal/gate/command.go:90`).
- Tests start Unix binaries by absolute path: `/usr/bin/printenv` and
  `/usr/bin/true` in `internal/shell/executor_test.go:659,690`; `/usr/bin/git`
  in the git fixtures of `cmd/agentplane` (`adapter_test.go:16`,
  `gate_test.go:23`, `loop_test.go:25`, `unit_test.go:26`,
  `changed_scope_test.go:19`, `progress_test.go:35`), `internal/runtime`,
  `internal/gitstate`, `internal/gate` and `internal/agent/codex`;
  `/usr/bin/mkfifo` in `cmd/agentplane/progress_test.go:42`; `/usr/bin/printf`
  in `cmd/agentplane/unit_test.go:70`.
- Tests assert Unix permission bits, which Windows does not report:
  `internal/api/docs_test.go:407,448` and `internal/store/store_test.go:65`.

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
