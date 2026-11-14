# AgentPlane desktop (Phase 2 vertical slice)

Electron shell and React renderer over the local `agentplane serve` sidecar
(F2.5, ADR-0019). The engine stays in Go; this app only observes and steers runs.

## Run it

```bash
make build                   # builds bin/agentplane (the sidecar)
cd desktop && npm ci
npm start                    # builds the renderer and opens the app
```

Pick `.agents/graphs/demo-vertical.yaml` under **New run**, press **Validate**,
then **Run**. Two branches run concurrently, `flaky` is rejected by its Gate and
retried alone, then the graph stops at the `approve` human node until you press
**Approve** (or **Reject**). Runs are stored under the app's user-data directory
(`AGENTPLANE_STORE_DIR` overrides it) and survive restarts.

## Checks

```bash
npm run check                # tokens in sync, typecheck, unit tests, production build,
                             # Storybook build, bundle scan (no Storybook code, no remote URLs)
make desktop-e2e             # real Electron + real sidecar under xvfb
make desktop-stories         # every story in a real browser, EN + PL: axe a11y, no network
```

`make desktop-e2e` uses `xvfb-run` when it exists (Linux); on macOS and Windows it
opens a real window, so do not touch the app while it runs. `make desktop-stories`
installs Playwright's Chromium once (`npx playwright install chromium`); set
`AP_CHROMIUM=/path/to/chrome` to use a browser you already have. Playwright is a
pinned dev dependency used only by these two checks.

## Storybook (development only, F2.5a)

```bash
npm run storybook            # http://127.0.0.1:6006, loopback only, no telemetry
npm run build-storybook      # static build into storybook-static/ (git-ignored)
```

Stories live next to the code: `src/ui/*.stories.tsx` (primitives) and
`src/screens.stories.tsx` (whole screens in named states). Screen stories run the
real components, reducer and selectors over **recorded event streams**
(`test/fixtures/*.json`, produced by `agentplane run --json`); a story never builds
node or run state by hand. A decorator installs an in-memory `window.agentplane`
(`src/storybook/bridge.tsx`), so no sidecar or network is involved. The toolbar
switches language (EN/PL); viewport is Storybook's built-in toolbar. Storybook is
a dev dependency and is never part of the packaged app.

## Design system and localisation (F2.5a)

- `src/design/tokens.json` is a copy of the Tarmind design system's `tokens.json`;
  `src/design/tokens.css` is generated from it (`npm run tokens`; `npm run check`
  fails if it is stale). `src/design/overrides.css` lists the few deliberate
  deviations (text colours that must reach 4.5:1 contrast).
- Geist and Geist Mono are bundled in `src/design/fonts/` (SIL OFL, licences next
  to the files); the CSP allows no remote font.
- UI text is localised in `src/i18n/` (`en.ts`, `pl.ts`, same keys; a test enforces
  it). Machine values (event types, node ids, paths, diagnostic codes and engine
  messages) are never translated. The language switch is in the top bar.
- Sidebar entries for Projects, Runners, Docs and Settings are intentionally
  greyed out and inert until those surfaces exist.

## Design rules

- **Node status is never guessed.** Everything on screen is a pure reduction of
  the event stream (`src/reducer.ts`). The map layout comes from the graph, the
  status from events; no component stores node state.
- **The renderer is sandboxed.** `contextIsolation` is on, `nodeIntegration` is
  off, the CSP allows only the bundle, and the sidecar port/token never leave
  the main process (`electron/main.cjs`). The preload exposes `request`,
  `subscribe` and `pickGraph` only, and the main process forwards only
  `/runs`, `/approvals` and `/graphs` paths.
- **Local only.** The sidecar binds `127.0.0.1`, requires a bearer token and
  exits when the app closes its stdin.

F2.5a R0 provides the shell, design tokens, primitives, Storybook and PL/EN
localisation. R1 redesigns Run Inspector (live graph card with Map / YAML / Split,
node detail with one attempt shown at a time), Approvals (inbox and review panes)
and New run (graph, inputs, preflight steps). The Dashboard is redesigned in R2.

Screens implemented from the Tarmind interface design: Runs (IA-01, list only),
Run Inspector (IA-15/16) with live map, attempts, reason/evidence and log,
Approvals (IA-17/18), New run with preflight (IA-12/13) and the YAML view of the
graph (IA-07-11, read-only). Not implemented: Project, Runners, Settings, mobile
approval, docs, onboarding, attempt compare, graph editing.
