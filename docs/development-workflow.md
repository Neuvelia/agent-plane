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
Go `make check`/build, full `go test -race ./...` and desktop `npm run check`
as separate jobs on pushes and pull requests. For concurrency changes, run
the affected race tests locally before the full gate. Browser story smoke and
real Electron e2e are separate checks; use `desktop/AGENTS.md` to choose them
for the affected behavior.

A green check is necessary, not sufficient. Acceptance criteria still need human and reviewer inspection.

## CI matrix

`.github/workflows/ci.yml` runs on pull requests, on pushes to `main` and on
manual runs (`workflow_dispatch`, any branch); a push to another branch runs
nothing. A newer push to a pull request cancels its older run; on `main` every
run completes. It runs `scripts/check.sh` (the body of `make check`) on three
runners: `linux-x64` (`ubuntu-latest`), `macos-arm64` (`macos-latest`) and
`windows-x64` (`windows-latest`). Linux and macOS are required and also run
`make build`. Windows is reported but non-blocking (`continue-on-error`, 20
minute cap) because main does not pass there yet; only that job checks out
with LF line endings so `gofmt -l` means the same on every OS. Remove
`experimental` for Windows once it is green. The desktop shell stays outside
this gate (`make desktop-check`).

A GitHub-hosted macOS minute costs ten Linux minutes and a Windows minute two,
so `check (macos-arm64)` and `check (windows-x64)` run on `main` and on manual
runs, and on pull requests only when the repository variable
`CI_OTHER_OS_ON_PR` is `true`. A macOS-only regression in a pull request is
therefore caught on `main` unless the author starts the workflow on the branch
by hand; do that for changes to process, path or Git handling.

A `detect` job picks the lanes a pull request runs from the paths it changes.
A change made only of Markdown files runs none, because no test or build reads
Markdown. Go jobs (`check`, `lint`, `race`) run for any other path outside
`desktop/`, and for `desktop/test/fixtures/`, which Go tests read. `desktop`
runs for `desktop/`, `.agents/` (the renderer bundles the templates) and
`.github/`. `desktop-e2e` runs for any non-Markdown path. Pushes to `main` and
manual runs run every lane. `All required checks pass` always reports and
fails when a lane failed or was cancelled; it is the check branch protection
should require, since a skipped lane never reports its own name.

Two desktop jobs run beside it. `desktop` runs `npm ci --ignore-scripts` and
`npm run check` (typecheck, reducer tests, build, Storybook build, bundle scan).
`desktop-e2e` first runs `npm ci` and installs the Playwright Chromium system
dependencies (which also provide xvfb), then `make desktop-e2e` (real Electron
against `bin/agentplane` under `xvfb-run`) and `make desktop-stories`
(Storybook smoke in Playwright Chromium). Both pin Node 24 and
use `npm ci`; `desktop-e2e` caches the npm cache, `~/.cache/electron` and
`~/.cache/ms-playwright`, and uploads `desktop/e2e-shots` as the
`desktop-e2e-screenshots` artifact on failure. `desktop-e2e` skips only
Markdown-only pull requests, so a sidecar or graph change is also exercised.
`desktop` is part of `All required checks pass`; `desktop-e2e` is not:
Go-only work does not wait on them unless branch protection says otherwise. If
the e2e flakes, capture a Playwright trace rather than retrying silently.

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
landed, so `experimental` stays until one run of that job is green.

## Self-hosted runners

Every job takes its runner from a repository variable holding a JSON `runs-on`
value; unset, it uses the GitHub-hosted runner. Deleting a variable switches
back.

| Variable | Jobs | Example value |
|---|---|---|
| `CI_RUNNER_LINUX` | `detect`, `check (linux-x64)`, `lint`, `race`, `desktop`, `desktop-e2e`, `All required checks pass` | `["self-hosted","linux","x64"]` |
| `CI_RUNNER_MACOS` | `check (macos-arm64)` | `["self-hosted","macOS","ARM64"]` |
| `CI_RUNNER_WINDOWS` | `check (windows-x64)` | `["self-hosted","Windows","X64"]` |
| `CI_OTHER_OS_ON_PR` | runs macOS and Windows on pull requests | `true` |

`scripts/ci/setup-linux-runner.sh` prepares an Ubuntu x64 machine: build tools
(including the C compiler `go test -race` needs, `jq` and `xvfb`), the
Playwright Chromium and Electron system libraries, an unprivileged runner
account and several runner services (`RUNNER_COUNT`, default 3; one runner
runs one job, and up to five Linux jobs of one run run at once). Run it as root with a
registration token from Settings, Actions, Runners. Go and Node are not
installed system-wide: `setup-go` and `setup-node` fetch the versions each job
asks for, so the machine needs outbound HTTPS to GitHub, the Go module proxy,
the npm registry, the Playwright CDN and nodejs.org.

On a self-hosted runner `desktop-e2e` installs only the Chromium browser
(`runner.environment` is not `github-hosted`), because the runner has no sudo
for `playwright install --with-deps`; the setup script installed those
libraries once. After a Playwright upgrade that needs new system libraries,
run the script again; configured runners are kept.

Constraints:

- Runners must not run as root: permission-bit tests skip as root and Electron
  needs `--no-sandbox`. The script refuses a runner account that can use sudo.
- Runners are persistent, not ephemeral: jobs share the runner account's home
  (`~/.cache/go-build`, `~/.cache/electron`, `~/.cache/ms-playwright`) and the
  tool cache. Tests use temporary directories, but a job can leave state for
  the next. Wipe or rebuild the machine if a run behaves differently from a
  clean one.
- A runner executes the code of every pull request and every branch someone
  starts the workflow on. The repository is private, so only collaborators can
  do that. Keep the machine free of credentials, provider sign-ins and access
  to internal networks; the CI jobs need no secrets. Restrict an organization
  runner group to this repository.
- macOS: register a runner on an Apple Silicon Mac with Xcode Command Line
  Tools under a standard (non-admin) account, as a launchd service (`./svc.sh
  install`). Windows: install Git for Windows so `shell: bash` resolves, and
  remember the job is still experimental.

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
