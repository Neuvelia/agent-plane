# AgentPlane desktop (Phase 2 vertical slice)

Electron shell and React renderer over the local `agentplane serve` sidecar
(F2.5, ADR-0019). The engine stays in Go; this app only observes and steers runs.

## Run it

```bash
make build                   # builds bin/agentplane (bin/agentplane.exe on Windows)
cd desktop && npm ci
npm start                    # builds the renderer and opens the app
```

Pick `.agents/graphs/demo-vertical.yaml` under **New run**, press **Validate**,
then **Run**. Two branches run concurrently, `flaky` is rejected by its Gate and
retried alone, then the graph stops at the `approve` human node until you press
**Approve** (or **Reject**). Runs are stored under the app's user-data directory
(`AGENTPLANE_STORE_DIR` overrides it) and survive restarts.

## Platforms and the sidecar binary

Main resolves the sidecar in `electron/sidecar.cjs`: `AGENTPLANE_BIN` if set,
otherwise `bin/agentplane` under the repository root (`bin/agentplane.exe` on
Windows, where `go build` appends `.exe`). `make build` uses `go env GOEXE`, so the
same target writes the right name on every platform; without `make`, run
`go build -o bin/agentplane.exe ./cmd/agentplane`. A missing binary stops startup
with an error naming the path that was tried and how to build it
(`test/sidecar.test.ts`).

Packaged app (not implemented): the repository-relative default holds only for a
checkout (`npm start`). A packaged app is expected to ship the engine under
`process.resourcesPath` and pass that path through the same resolver; that choice
is deferred until packaging is activated.

Verified platforms:

| Platform | Status |
| --- | --- |
| Linux (xvfb) | `npm run check` and `make desktop-e2e` |
| Windows 11 | engine build, `npm run check`, and the full `e2e/demo.cjs` (with the Windows variant described below) |
| macOS | not verified |

Windows notes (Windows 11, Go 1.27, Node 24, real window, unattended): `go build`
wrote `bin/agentplane.exe`, `npm ci` and `npm run check` passed, and `node e2e/demo.cjs`
ran to the end (two consecutive runs). `e2e/demo.cjs` is platform-aware: the engine
binary comes from `binaryName`, temporary paths from `os.tmpdir()`, and the
connection-lost step kills only the sidecar process that is a descendant of the
launched app, by PID (`ps` on Linux, a process table query on Windows). The demo
graph and the editor round trip name POSIX tools (`/usr/bin/printf`, `/bin/sh`) by
absolute path, which Windows does not have, so on win32 the script derives the same
graph in its temp directory with the shell commands replaced by the running `node`
binary; the structure, loop, Gate and human node are unchanged. Steps that need
Agent or command Gate processes (codex adapter, `/usr/bin/diff`) are not part of this
scenario. Not verified on Windows: the missing-binary dialog in a real window (covered
by the unit test). The Linux e2e still passes under `xvfb-run` after these changes.
Shutdown itself is portable: closing the sidecar's stdin ends it, and the 1.5 s
`kill()` fallback maps to `TerminateProcess`.

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

CI (`.github/workflows/ci.yml`): job `desktop` runs `npm run check`; job
`desktop-e2e` runs both commands above under xvfb with Node 24, caches the npm
cache, `~/.cache/electron` and `~/.cache/ms-playwright`, and uploads the e2e
screenshots (`make desktop-e2e E2E_SHOTS=e2e-shots`, git-ignored) as an artifact
on failure. Locally, pass `E2E_SHOTS=<dir>` to save them.

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
  `subscribe`, `pickGraph` and the graph-file calls (`openGraph`, `newGraph`,
  `recentGraphs`, `openRecentGraph`, `touchRecent`, `closeGraph`), and the main
  process forwards only an exact list of routes (`electron/allowlist.cjs`):
  never `/graphs/grants`, which only main calls. Graph files for the editor are
  chosen in native dialogs in main (`electron/graphs.cjs`) and reach the
  renderer only as opaque grants and file names, never paths (ADR-0021). Recently
  opened graphs are kept by main in the user data directory
  (`electron/recents.cjs`).
- **Recording run values is opt-in.** Settings has a "Record run values" switch,
  off by default (ADR-0023). Main keeps it in user data (`electron/recording.cjs`)
  and starts the sidecar with `--record-values` and `--redact-env NAME` for the
  listed variables; a change applies at the next app start. An unset listed
  variable keeps recording off for that session. The renderer reaches it only
  through `recordingSettings`/`setRecordingSettings` and never sees environment
  values.
- **Local only.** The sidecar binds `127.0.0.1`, requires a bearer token and
  exits when the app closes its stdin.

## YAML editor (F2.1, slice E2.1)

The Editor entry opens a graph file through the native dialog, shows its YAML in
CodeMirror 6 and saves it with Ctrl/Cmd+S. The draft, undo/redo history, dirty
state and conflict handling live in `src/editor/doc.ts` and
`useGraphDocument.ts`; validation is the engine's (`POST /graphs/validate`), the
renderer holds no graph rules. Saving sends the hash of the text it opened
(`base_hash`); a file changed on disk is reported as a conflict and is never
overwritten silently. A new graph is create-only. The canvas, Visual/Split view
and "Save and run" come in later slices.

## Canvas (F2.1, slice E2.2)

The Editor shows the graph beside its YAML (Visual / YAML / Split). The canvas is
React Flow with one handle per declared port. Every gesture becomes an ADR-0020
operation sent to `POST /graphs/edit` with the text on screen; the answer replaces
the draft as one undo step. Whether a connection is allowed is the engine's
`can_connect` verdict, asked once when a drag starts and shown on the target
handles; nothing in `src/editor` decides it (`test/no-graph-rules.test.ts`).
Moving a node writes `metadata.layout` (ADR-0022), which never changes the
semantic version. While the YAML does not parse the canvas keeps the last good
graph read-only; files with anchors, aliases or merge keys open read-only too.
Everything is reachable from the keyboard: the node list, "Connect…" with its
engine-checked target list, Rename, Delete and the connections list.

## Issues panel (F2.4, slice E3.2)

The Issues card lists the engine's diagnostics as received: errors first, then
warnings, then by element, code and message (`src/editor/issues.ts`). Filter
All / Errors / Warnings; each issue is a button. Choosing one selects its node,
edge, port or binding on the canvas, puts the caret on its YAML line (when the
YAML is shown) and moves keyboard focus to the selection's heading; a graph-level
issue focuses the graph header. Nodes, edges and ports carry an error or warning
badge (a glyph and a word, never colour alone). While a result is out of date or
pending the list is kept and marked; if the engine cannot be reached the list is
marked out of date and running is blocked until a check succeeds. "Save and run"
names the reason it is blocked ("2 errors") and offers "Show first error".
Warnings never block a run. A text that does not parse shows one issue with its
line.

## Save and run (F2.1, slice E2.3)

"Save and run" saves a changed draft first (a refused save, such as a conflict,
stops there and starts nothing) and then starts a run with `graph_grant`, so the
run always uses the saved bytes, never unsaved text. It is available when the
engine reports no error on the checked draft and the graph has nodes; the reason
it is not available is shown next to the toolbar. Graphs that declare inputs show
a `name=value` field. On window focus the editor looks at the file: a clean draft
follows it silently, a dirty draft gets the conflict banner (Keep mine / Reload).

CodeMirror (`@codemirror/state`, `view`, `language`, `lang-yaml`, `commands`,
`@lezer/highlight`, all MIT, exact-pinned) is a renderer-only, development-time
bundle dependency: it makes no network requests and is loaded lazily, so the
editor chunk (about 106 kB gzipped) is not part of the first paint. A plain
`<textarea>` cannot give accessible YAML highlighting, line numbers, or a
programmable keymap without reimplementing an editor.

F2.5a R0 provides the shell, design tokens, primitives, Storybook and PL/EN
localisation. R1 redesigns Run Inspector (live graph card with Map / YAML / Split,
node detail with one attempt shown at a time; a standalone Gate node has no attempts,
so its single `gate_check`, which carries no attempt ID, is kept on the node and shown
there), Approvals (inbox and review panes)
and New run (graph, inputs, preflight steps). R2 adds the Dashboard (needs attention,
active runs, 7-day outcomes, quick actions) and, in the inspector, a run summary
(wall clock, node time, saved, retries, tokens always unknown), a per-second
timeline and a filterable event log. Those numbers are computed in the app from
the event stream (`src/insight.ts`, `src/dashboard.ts`) and carry no authority
over run state.
R3 adds loading and error states for every engine-backed screen (a failed read is
an error with Retry, never an empty list), a skip link and focus-to-content on
navigation, contrast-checked colours and reduced-motion support. F2.5a is
implemented; screenshots are in `docs/validation/F2.5a-screenshots/`.

Screens implemented from the Tarmind interface design: Runs (IA-01, list only),
Run Inspector (IA-15/16) with live map, attempts, reason/evidence and log,
Approvals (IA-17/18), New run with preflight (IA-12/13) and the YAML view of the
graph (IA-07-11, read-only), plus Settings with the interface language and the opt-in "Record run values" switch. Not
implemented (greyed out in the sidebar where they would live): Projects, Runners,
Docs; also mobile approval, onboarding, attempt compare and graph editing.
