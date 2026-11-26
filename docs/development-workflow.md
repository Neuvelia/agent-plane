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

`make verify-changes` prints a read-only JSON inventory of local staged,
unstaged and non-ignored untracked paths. Each path has sorted `sources`, and
the document has `checks_executed: false`. A successful exit means the
inventory was collected; it does not mean tests were selected, run or passed.
This command is a foundation for later selective verification and does not
replace `make check`.

For changes under `desktop/`, also run `npm run check` there. CI reports the
Go lint, `make check`, full `go test -race ./...` and build in one Linux job,
and desktop `npm run check`, Electron e2e and Storybook smoke in another (see
"CI matrix"). For concurrency changes, run the affected race tests locally
before the full gate. Use `desktop/AGENTS.md` to choose the local browser story
smoke and Electron e2e for the affected behavior.

A green check is necessary, not sufficient. Acceptance criteria still need human and reviewer inspection.

## CI matrix

CI runs only the jobs a change can fail. `.github/workflows/ci.yml` runs on pull requests, on pushes to `main` and on
manual runs (`workflow_dispatch`, any branch); a push to another branch runs
nothing. A draft pull request runs nothing either: every job skips until it
is marked ready for review, which starts a run (start the workflow by hand on
the branch for earlier feedback). A newer push to a pull request cancels its
older run; on `main` every run completes.

A `detect` job picks the jobs a pull request runs from the paths it changes. A
change made only of Markdown files runs none, because no test or build reads
Markdown. `check (linux-x64)` and `check (macos-arm64)` run for any other path
outside `desktop/` and for `desktop/test/fixtures/`, which Go tests read. `desktop` runs for
`desktop/`, the sidecar sources (`cmd/`, `internal/`, `go.mod`, `go.sum`,
`Makefile`), `.agents/` and `.github/`. Pushes to `main`, manual runs and a
failed `detect` run every job. A skipped job counts as passed for a required
check.

`check (linux-x64)` holds all Linux Go evidence in one job: `golangci-lint`
(findings new since the merge base, so it checks out the full history), then
`scripts/check.sh` (the body of `make check`), `go test -race ./...` and
`make build`. A lint failure does not stop the later steps.
`check (macos-arm64)` (`macos-latest`) runs `scripts/check.sh` and `make
build`; lint and `-race` stay Linux-only. Linux and macOS are required targets.

`desktop` runs `npm ci` and `npm run check` (typecheck, reducer tests, build,
Storybook build, bundle scan), builds `bin/agentplane`, installs the Playwright
Chromium system dependencies (which also provide xvfb), then runs the real
Electron app against the sidecar under `xvfb-run` (`e2e/demo.cjs`) and the
Storybook smoke (`npm run stories:test`) on the `dist/` and
`storybook-static/` the check built. It pins Node 24, caches the npm cache,
`~/.cache/electron` and `~/.cache/ms-playwright`, and uploads
`desktop/e2e-shots` as the `desktop-e2e-screenshots` artifact (kept 7 days)
when the e2e fails. The e2e and smoke steps are not blocking: a failure shows
as a failed step and a warning, not a failed job. If the e2e flakes, capture a
Playwright trace rather than retrying silently.

`.github/workflows/ci-other-os.yml` runs `scripts/check.sh` on `windows-x64`
(`windows-latest`), only when started by hand on a branch (Actions,
`ci-other-os`, Run workflow). Windows is not yet a supported target because
main does not pass there. Only that job checks out with LF line endings so
`gofmt -l` means the same on every OS. The desktop shell stays outside this
gate (`make desktop-check`).

The Go tests compile on every OS. A test that needs something Windows lacks
skips with a reason, using the test-only `internal/testenv` guards:
`RequireExecutable` for absolute Unix tool paths (`/usr/bin/git`, `/bin/sh`,
...), `RequireDarwinOrLinux` for components that reject other platforms by
design, and `RequireUnixPermissions` for permission-bit assertions. On Linux
and macOS nothing is skipped. Skips on Windows mark product work that waits
for an owner decision on native Windows support:

- Production code resolves Git from `PATH` (`gitlocate.ResolveGit`), but test helpers still start
  `/usr/bin/git`, so git-evidence and changed-files tests skip.
- The Codex adapter accepts only Darwin and Linux
  (`internal/agent/codex/adapter.go:75`).
- The command Gate requires `/usr/bin/diff` and `/dev/fd/3`
  (`internal/gate/config.go:62`, `internal/gate/command.go:63,90`).
- Cancelling a shell child that already exited fails with "TerminateProcess:
  Access is denied", which `internal/shell/executor.go` (around line 136)
  reports as an exit failure instead of cancellation.
- Shell-node fixtures and `.agents/graphs/demo-vertical.yaml` start
  `/usr/bin/printf`, `/usr/bin/printenv` and `/bin/sh`.

Tests that cannot hold on Windows skip with a reason rather than fail:
`testenv.RequireUnixPermissions` and `testenv.HasUnixPermissions` for file-mode
assertions (`internal/api` save tests), and `testenv.SkipOnWindows` for
fixtures the platform cannot create (a file name with a newline, an invalid
UTF-8 argv byte in `scripts/verifychanges`). Path comparisons against a child's
working directory resolve both sides with `filepath.EvalSymlinks`, so 8.3 short
names (`RUNNER~1`) compare equal.

A local `go test ./...` on Windows (Go 1.27) passes with these guards. The
`windows-x64` job has not been observed green on a GitHub runner since they
landed; it stays manual-only until one run of that job is green.

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
