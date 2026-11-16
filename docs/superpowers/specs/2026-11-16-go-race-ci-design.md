# H18: Go race check in CI

## Intent

The Go runtime, event collector and API have concurrent execution paths, but
the current CI runs only ordinary `make check` and `make build`. Many active
feature specs and ADRs require `go test -race ./...` as a release check. Make
that deterministic check visible on every push and pull request.

## Approaches

- **Separate full race job (chosen):** run `go test -race ./...` alongside the
  existing Go and desktop jobs. It gives a distinct status and does not extend
  the latency of the ordinary Go job.
- **Add a step to the Go job:** less YAML, but ordinary test/build results and
  race results are coupled and harder to diagnose independently.
- **Target only selected packages:** faster, but misses a race introduced by
  cross-package integration or a later package. A selector can be considered
  after measuring full-job cost.

## Scope and contracts

- Add one `race` job to `.github/workflows/ci.yml` under the existing `push`
  and `pull_request` triggers. Keep existing job steps and `make check`
  unchanged.
- Use GitHub-hosted Ubuntu, the repository's Go version from `go.mod`, Go
  module caching and the exact command `go test -race ./...`.
- Checkout must not persist Git credentials. Inherit the workflow's
  `contents: read` permission; add no secrets, `pull_request_target`, remote
  services or third-party test dependency.
- Update `docs/development-workflow.md` to show that CI has separate ordinary
  Go, Go race and desktop results. The root developer gate stays `make check`;
  local race runs remain relevant when changing concurrency behavior.

## Acceptance

- Workflow parses and keeps the existing triggers and jobs; the new job runs
  the exact full race command with checkout credentials not persisted.
- Local `go test -race ./...` and `make check` pass on the H18 tree.
- No product code, graph/event/API contract or dependency changes appear in
  the diff.
- The H18 PR's hosted `race`, ordinary Go and desktop jobs all pass on the
  same head commit before completion is claimed.

## Risks and limits

The race detector increases compile time and memory. A separate job keeps its
cost visible and avoids hiding failure behind ordinary tests. It detects data
races exercised by tests, not every possible concurrency bug. On an untrusted
PR it runs repository code on a hosted runner with read-only permissions and
no persisted checkout credentials.
