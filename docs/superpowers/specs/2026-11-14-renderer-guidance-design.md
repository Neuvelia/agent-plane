# H6: Renderer-local agent guidance

## Intent

An agent changing the desktop renderer should find state ownership and the
corresponding tests quickly. The root `AGENTS.md` remains the repository-wide
policy; the sibling Electron guide covers main/preload trust boundaries.

## Approach

Add `desktop/src/AGENTS.md` for bridge calls, event reduction, polling,
connection handling, derived insight, design and the high-level editor
boundary. Link current specs, ADRs and focused tests. A later guide can cover
the editor's detailed draft/operation behavior without enlarging this step.

This changes contributor guidance only. No product code, public API, event
schema, dependency or CI rule changes.

## Scope and acceptance

- Every named source, test, spec and ADR path exists on the target branch.
- The guide separates run/node state derived from events, API list state and
  local editor draft state.
- It preserves replay idempotency, stream-loss handling, bounded display
  state and engine-owned graph validation/operations.
- It points to current PL/EN, token, story and accessibility checks without
  claiming unimplemented F2.1 slices are present.
- An independent reviewer can locate a duplicate-event/connection-loss
  regression and a stale editor draft/invalid connection regression.
- Focused renderer tests, `npm run check` and `make check` pass.

## Risks

F2.1 E2.2 landed while this guide was planned. Describe current code, not
future Save and run or form behavior. The renderer does own local editor draft
and selection state; wording that forbids all component state would be false.
