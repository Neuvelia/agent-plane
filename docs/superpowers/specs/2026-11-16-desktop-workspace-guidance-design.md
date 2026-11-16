# H16: Desktop workspace agent guidance

## Intent

An agent changing the desktop should know which process owns each contract,
which generated files must stay in sync and which checks actually exercise
unit code, Storybook in a browser and the real Electron app.

## Approach

Add `desktop/AGENTS.md` at the package root. It supplements existing Electron,
renderer and editor guides without repeating their detailed contracts. Ground
the command matrix in `Makefile`, `desktop/package.json`, the desktop scripts and
current tests. Describe committed tokens and recorded fixtures separately from
ignored build output.

This changes contributor guidance only. It does not alter product behavior,
dependencies, graph/API/event contracts or CI.

## Scope and acceptance

- Every named source, script, test, feature and ADR path exists.
- The guide assigns Go execution, Electron main and renderer authority to
  their current owners and points to the narrower local guides.
- It states that root `make check` is Go-only, `npm run check` includes the
  Storybook build but not browser smoke, and real Electron and story checks
  have separate targets.
- It identifies generated committed token CSS and recorded API fixtures,
  while keeping ignored build output out of PRs.
- It preserves local-only renderer resources, the preload trust boundary,
  recorded event/detail fixtures versus synthetic presentation inputs and
  PL/EN catalogue parity.
- `npm run check` and root `make check` pass.

## Risks

The desktop README mixes current behavior with older slice descriptions.
Describe current commands from `package.json` and `Makefile`, and use active
feature specs and code for implementation status. In particular, a successful
Storybook build does not prove browser accessibility or no-network smoke, the
bundle scan does not inspect JS network calls, and the Go-only root check does
not validate the desktop package.
