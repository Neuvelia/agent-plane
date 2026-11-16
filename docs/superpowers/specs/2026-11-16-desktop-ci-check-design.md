# H17: Desktop check in CI

## Intent

The repository's CI currently checks and builds Go only. A desktop change can
merge without CI running its token-sync check, TypeScript typecheck, unit
tests, renderer build, Storybook build or bundle scan. Run the existing
desktop check on every push and pull request, with a separate result from Go.

## Approaches

- **Separate desktop job (chosen):** install the locked npm dependencies and
  run `npm run check` in `desktop/`. Go and desktop failures remain distinct,
  and neither job waits for the other.
- **Extend the Go job:** fewer YAML lines, but desktop setup lengthens the Go
  gate and obscures which stack failed.
- **Changed-path filter:** avoids some installs, but can miss cross-package
  changes that affect desktop contracts. Defer until measured CI cost warrants
  a dependency-aware selector.

## Scope and contracts

- Add one `desktop` job to `.github/workflows/ci.yml` under the existing
  `push` and `pull_request` triggers. Keep the Go job and `make check` unchanged.
- Use GitHub-hosted Ubuntu, read-only repository permissions, checkout without
  persisted credentials and Node 24. The locked Vite version accepts Node
  22.12 or newer. Cache npm packages using `desktop/package-lock.json`.
- Install with `npm ci --ignore-scripts` if the existing desktop check passes
  after that install; this avoids running dependency lifecycle scripts during
  install. Then run `npm run check` from `desktop/`.
- Do not pass secrets, use `pull_request_target`, execute a real Electron app,
  download a browser or change product code. Browser stories and e2e remain
  separate, explicitly named checks for applicable work.
- Update `desktop/AGENTS.md` and `docs/development-workflow.md` to describe
  the two CI jobs and the checks they do and do not cover.

## Acceptance

- Workflow parses, retains both triggers, and has independent Go and desktop
  jobs. The desktop job uses the nested lockfile and runs `npm run check`.
- A fresh `npm ci --ignore-scripts` followed by `npm run check` passes locally.
- Root `make check` passes; no product code, API/event schema or dependency
  changes appear in the diff.
- The H17 PR's actual GitHub desktop and Go checks pass before completion is
  claimed. If hosted CI exposes a platform-specific failure, resolve it in
  this PR and rerun both checks.

## Risks and limits

GitHub Actions runs untrusted PR code on a hosted runner. Keep permissions
read-only, avoid persistent checkout credentials and do not inject secrets.
Ignoring install scripts is viable only if the locked toolchain's existing
desktop check still passes. `npm run check` builds Storybook but does not run
its browser smoke or real Electron e2e; do not claim those gates from this job.
