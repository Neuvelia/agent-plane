# H4: API-local agent guidance

## Intent

An agent changing the local API should quickly find the owner of a route or
state boundary and its regression tests. The root `AGENTS.md` remains the
repository-wide policy.

## Approach

Add `internal/api/AGENTS.md` near the API implementation. Map the server,
hub, document, DTO, CLI composition and Electron forwarding owners. State the
existing contracts for authenticated loopback access, store-first event delivery,
first-decision-wins approval, and grant-scoped graph writes. Link the active
feature specs, ADRs and targeted tests.

This is guidance for contributors, not a change to the AgentPlane product or
its public API. No codebase-map change is needed: it already names the API and
desktop boundaries accurately.

## Scope and acceptance

- Every named source, test, spec and ADR path exists on the target branch.
- The guide distinguishes the API token/Host check from Electron's renderer
  route allowlist. It does not claim the API authenticates an Electron-main
  identity.
- The guide preserves the existing path-based read/run compatibility and
  identifies grant-only writes and saved-file run behavior.
- Event replay, persistence failure, approval idempotency and save conflict
  rules match implementation and focused tests.
- An independent reviewer can use it to locate a duplicate-approval or SSE
  regression and a stale-save or renderer grant-access regression.
- `go test ./internal/api/...` and `make check` pass. No product code, public
  contract, dependency or CI rule changes.

## Risks

API and Electron behavior can change in separate slices. Keep this guide
aligned with current code, especially when a new route, bridge method or
document field is added. The ADR describes a directory sync after save;
current code attempts it on a best-effort basis, so the guide does not claim
an unconditional power-loss durability guarantee.
