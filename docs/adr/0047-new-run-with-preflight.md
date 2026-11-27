# ADR-0047 - New run with a live preflight replaces the stepped New run form

**Status:** proposed
**Date:** 2026-11-27
**Amends:** F2.5a requirement 17 (New run)
**Feature:** F2.5c (`docs/features/F2.5c-new-run-preflight.md`)

## Context

F2.5a requirement 17 made New run a stepped form: choose a file, press Validate, type inputs as
`key=value` lines, read a diagnostics card, press Start run. The Tarmind canvas artboard "New run
with preflight" replaces it with a two-column screen whose preflight reacts to every choice and
also covers Agent-specific risks (Codex availability, the agent writing directly into the folder the graph names).
The artboard also shows per-run options that the engine does not accept yet.

## Decision

1. New run follows the artboard (F2.5c). Choosing a graph validates it and opens its document;
   there is no separate Validate button. Inputs are one field per declared graph input instead of
   free text.
2. The preflight is a view of engine answers only: validation status and diagnostics, the
   capability report and the graph definition (declared inputs, node kinds, the Agent node's
   `isolation` value as written). Only the engine's validation status enables Run; other rows warn.
3. Options the engine cannot take per run (max concurrency, Agent workspace) are shown disabled
   with their current source. Making them work is a separate API decision (#209, #210).

## Alternatives considered

### Hide the Options group until the API supports it

Pros: nothing on screen that does not work.

Cons: diverges from the artboard; the person cannot see where these values come from today.

### Add the options to `POST /runs` now

Pros: full artboard behaviour.

Cons: changes the API and workspace-isolation semantics (F3.1); needs its own ADR and engine work.

## Consequences

### Positive

- Fewer steps; the person sees missing inputs and Agent risks before starting.
- No engine, API, event or trust-boundary change.

### Negative / trade-offs

- One extra `POST /graphs/open` per chosen graph.
- Inputs not declared by the graph can no longer be typed (the engine rejects them anyway).
- Two disabled controls until #209/#210 land.

## Compatibility and migration

No stored data, graph, event or API changes. The `validate` test ID is gone; `choose`, `run`,
`graph-label`, `preflight-status`, `preflight-card`, `diagnostic` and `validating` remain. The
"Ready" status text became "Ready to run"; the e2e script follows.

## Validation

`desktop/test/new-run.test.tsx`, the New run stories in the EN/PL story smoke (axe) and the
real-Electron e2e (`desktop/e2e/demo.cjs`).
