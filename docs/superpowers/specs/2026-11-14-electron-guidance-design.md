# H5: Electron-local agent guidance

## Intent

An agent changing Electron main or preload should find the boundary owner,
the applicable product contract and focused regression tests without having
to infer trust rules from several files. The root `AGENTS.md` remains the
repository-wide policy.

## Approach

Add `desktop/electron/AGENTS.md` for the sidecar, IPC, file-grant and
renderer isolation boundaries. Link existing F2.5 and F2.1 contracts, ADRs
and tests. Keep the guide in the main/preload package; renderer presentation
can get its own small guide later.

This changes contributor guidance only. No product code, public API, event
schema, dependency or CI rule changes.

## Scope and acceptance

- Every named source, test, spec and ADR path exists on the target branch.
- The guide locates the exact request allowlist and separate SSE subscription
  path, as well as main's sidecar token and process lifecycle.
- It keeps editor writes behind main-issued grants without claiming that the
  legacy `pickGraph`/`graph_path` run flow is grant-only.
- It identifies recent-file reopen, grant revocation and failure behavior
  that current tests cover.
- An independent reviewer can use it to find where to change a renderer
  request route and how to test a grant-access or recent-file regression.
- Focused desktop tests, `npm run check` and `make check` pass. The real
  Electron e2e is reserved for behavior changes to IPC, process or file paths.

## Risks

The F2.1 spec includes future canvas slices. The guide must state only
current behavior. A generic statement that the renderer never sees any path
would be false while the legacy New run picker exists. The Go sidecar owns
grant validation and save behavior; this guide should not assign those checks
to Electron.
