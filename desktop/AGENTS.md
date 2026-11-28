# Desktop workspace guidance

The root `AGENTS.md` applies here. This guide covers the desktop package as a
whole: build/check commands, generated assets and cross-process ownership.
For implementation details, follow the narrower guides in `electron/`, `src/`
and `src/editor/`. Read `desktop/README.md` and the active feature spec before
changing behavior; verify older roadmap statements against current code.

## Find the owner

| Concern | Start here |
| --- | --- |
| Dependency versions and desktop commands | `package.json`, `package-lock.json` |
| Renderer bundling, TypeScript and resource policy | `vite.config.ts`, `tsconfig.json`, `index.html` |
| Sidecar lifecycle, IPC and grants | `electron/AGENTS.md` |
| Event-derived views, design and localisation | `src/AGENTS.md` |
| Graph draft, canvas and validation UI | `src/editor/AGENTS.md` |
| Runtime app icon assets (`build/`; packaging is not set up yet, issue #242) | `build/`, `electron/appIcon.cjs` |
| Design-token generation and bundle scan | `scripts/sync-tokens.mjs`, `scripts/check-bundle.mjs` |
| Storybook configuration and browser smoke | `.storybook/`, `scripts/stories-smoke.cjs` |
| Recorded API fixtures and screenshots | `scripts/record-graph-fixtures.cjs`, `scripts/screenshots.cjs` |
| Desktop unit, real-app and fixture tests | `test/`, `e2e/demo.cjs` |

## Preserve these boundaries

- The Go engine remains the execution authority. Electron main owns the
  sidecar, token, native file choices and IPC allowlist; the renderer consumes
  the narrow preload bridge and derives run state from events. A desktop build
  must not introduce direct renderer network/process/filesystem access or a
  second scheduler. Coordinate cross-boundary contract changes with the
  owning Go package, feature spec and ADR rather than patching only the UI.
- Keep `index.html` CSP and Vite's relative `base: './'` compatible with
  loading the renderer from Electron's local file. Storybook is development
  only: its code must stay out of the production renderer bundle, and that
  bundle must not load remote resources. `scripts/check-bundle.mjs` scans
  built JS/CSS/HTML for Storybook markers and CSS/HTML for remote resource
  URLs. It does not detect network calls inside JS; keep the CSP restrictive
  and use real-browser checks for runtime requests.
- `src/design/tokens.json` is the copied design-token source and
  `src/design/tokens.css` is committed generated output. After changing the
  JSON, run `npm run tokens`, inspect the CSS diff and keep both files in sync.
  Every colour token needs a `light` value too (ADR-0047); the contrast test
  checks both themes.
  Preserve the documented overrides, local font files and licences. UI copy
  belongs in both PL/EN catalogues; machine values and engine messages stay
  untranslated. Use existing tests for token, catalogue and contrast parity.
- Screen stories use real components and recorded event streams for detailed
  run/node state through the in-memory Storybook bridge; they do not contact
  the sidecar. Graph-document API responses are recorded too, while graph
  summaries, run-list summaries and some failure scenarios are synthetic
  presentation inputs. Do not describe all Storybook API state as recorded.
  `scripts/record-graph-fixtures.cjs` writes graph-document responses into
  `test/fixtures/` from a built Go sidecar. After re-recording, inspect the
  full diff and commit only intended fixture changes; do not hand-edit
  recorded responses to make a screen pass. `scripts/screenshots.cjs` writes
  validation images when a feature explicitly calls for updated screenshots.
- Keep package and lockfile changes together when changing dependencies.
  `npm ci` installs from the lockfile; `node_modules/`, `dist/` and
  `storybook-static/` are ignored build artifacts, not PR content. Do not
  add a runtime dependency merely to support a development check.

## Run the right checks

- For a desktop change, run focused `npm test` cases, then `npm run check`
  from `desktop/`, plus root `make check`. The root check covers Go only;
  CI reports independent Go and desktop jobs. `npm run check` checks token
  sync, typecheck, unit tests, renderer build, Storybook build and the bundle
  scan. The desktop CI job installs from the lockfile without dependency
  lifecycle scripts before running that command.
- `npm run check` builds Storybook but does not run browser story smoke.
  For changes to stories, shared UI, localisation or accessibility, run
  `make desktop-stories` when Chromium is available. It renders every story
  in EN and PL, in the dark and light themes, with accessibility and
  no-external-request checks, and drives the profile menu (F2.5c).
- For changes to Electron IPC, sidecar lifecycle, graph-file boundaries or
  end-to-end run flows, run `make desktop-e2e` when the required display is
  available. It uses a real Electron window and Go sidecar; on macOS and
  Windows avoid interacting with the window during the run. The target builds
  Go and the renderer first.
- `make desktop-check` runs Go build, `npm ci` and `npm run check`. Use it for
  a clean dependency-install path; it is separate from root `make check`.
  Do not claim story-browser or real-Electron coverage from `npm run check`.

Read `docs/features/F2.5-live-map.md`,
`docs/features/F2.5a-desktop-redesign.md` and
`docs/adr/0019-local-run-store-and-loopback-api.md` for the desktop baseline.
For graph editing, also read `docs/features/F2.1-graph-canvas.md` and
`docs/adr/0021-local-graph-document-api-and-write-trust.md`.
Update this guide when the workspace checks, generated outputs or boundary
owners change.
