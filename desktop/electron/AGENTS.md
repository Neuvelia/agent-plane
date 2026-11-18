# Electron boundary guidance

The root `AGENTS.md` applies here. This file adds rules for
`desktop/electron/` source. Read `desktop/README.md` and the active feature
spec before changing an IPC or API boundary.

## Find the owner

| Concern | Start here |
| --- | --- |
| Sidecar lifecycle, token, API forwarding, SSE and window setup | `main.cjs` |
| Exact renderer request routes | `allowlist.cjs` |
| The narrow `window.agentplane` bridge and subscriptions | `preload.cjs` |
| Native graph dialogs, grant registration and revocation | `graphs.cjs` |
| Recent graph paths held in main-process user data | `recents.cjs` |
| "Record run values" preference and `serve` flags (ADR-0023) | `recording.cjs` |
| `serve --codex-bin` from an absolute `AGENTPLANE_CODEX_BIN` (F2.11) | `capabilities.cjs` |
| Renderer bridge types and API wrappers | `desktop/src/types.ts`, `desktop/src/api.ts`, `desktop/src/editor/api.ts` |
| Go API routes, grant checks and run decisions | `internal/api/AGENTS.md` |

## Preserve these contracts

- Electron main owns the `agentplane serve` process and its loopback port and
  bearer token. The renderer uses the preload bridge; do not expose the token,
  sidecar address, Node APIs or a general network/filesystem primitive to it.
  Keep `contextIsolation`, the renderer sandbox, navigation/window blocking and
  the `desktop/index.html` CSP aligned with this boundary. Do not forward
  sidecar stderr or graph content to logs.
- `ap:request` must pass only exact method/path pairs from `allowlist.cjs`.
  Keep `/graphs/grants` and its revocation route inaccessible through generic
  renderer requests; adding an API route must not make it reachable by prefix.
  SSE uses separate `ap:subscribe`/`ap:unsubscribe` IPC and main-process
  authenticated `fetch`, since `EventSource` cannot set the bearer header.
  Abort subscriptions on unsubscribe, renderer navigation and app shutdown;
  pass stream endings to the renderer without inventing run/node state.
- Open/new graph file choices occur in main through native dialogs. Main
  registers the chosen path with the sidecar and returns only an opaque grant,
  label and existence flag to the renderer. A recent item is reopened from
  main's stored path with a fresh grant; the recent list exposes only ids and
  labels. `touchRecent` accepts only grants main issued; `closeGraph` requests
  revocation and reports whether it succeeded. Recents are a convenience:
  read/write failure must not block editing.
- New run chooses its graph with `openGraph()` and holds only the grant;
  there is no bridge call that returns a path. `isBodyAllowed` (in
  `allowlist.cjs`) forwards `POST /runs` and `POST /graphs/validate` only with
  the body keys `graph_grant`, `inputs` and `grant`, `text`; keep it a key
  allow-list, since the Go decoder matches JSON keys case-insensitively. The Go
  API still accepts `graph_path`/`path` for non-renderer clients (tests,
  scripts), so do not remove them from it. Keep graph-document writes
  grant-only; the Go API owns path checks, conflict detection and atomic save. Electron main and
  preload do not implement graph validation, scheduling, retry or run-state
  reduction; the renderer derives run state from events.
- "Record run values" is a main-process preference in user data
  (`recording.json`, 0600), off by default and read when the sidecar starts;
  a change applies at the next start, never to the running sidecar (whose
  grants and runs would be lost). Main validates the shape strictly (a
  boolean and at most 16 plain variable names) and passes `--redact-env`
  only for names set in its environment, plus `--record-values` only when
  enabled and no configured name is unset, so `serve` always starts. The
  renderer gets the preference, whether this session records and the unset
  names, never environment values; it uses the dedicated
  `ap:recordingSettings`/`ap:setRecordingSettings` IPC, not `ap:request`.
- Keep the sidecar's stdin connected for shutdown, cancel streams before
  quit, and retain the bounded forced-kill fallback. Sidecar/API failures
  must remain visible as connection or request errors rather than a guessed
  execution outcome.

The contracts are in `docs/features/F2.5-live-map.md`,
`docs/features/F2.1-graph-canvas.md`,
`docs/adr/0019-local-run-store-and-loopback-api.md`, and
`docs/adr/0021-local-graph-document-api-and-write-trust.md`. F2.1 is being
delivered in slices: use current code and the active slice, not future canvas
requirements, when describing implemented behavior.

## Choose regression tests

- Generic route exposure, path escapes and refused path bodies: `desktop/test/allowlist.test.ts`.
- Graph dialogs, grants and revocation: `desktop/test/graphs-handlers.test.ts`.
- Recent-list privacy, ordering and failure: `desktop/test/recents.test.ts`.
- Recording preference, validation and `serve` flags: `desktop/test/recording.test.tsx`.
- Capability routes and `--codex-bin`: `desktop/test/allowlist.test.ts`,
  `desktop/test/capabilities-flags.test.ts`.
- Real bridge, sidecar, API and editor path: `desktop/e2e/demo.cjs`.
- API auth, SSE and grant checks: `internal/api/server_test.go`,
  `internal/api/docs_test.go`.

Run the focused desktop tests first, then `npm run check` in `desktop/` and
repository `make check`. For a changed IPC, process or file boundary, also run
`make desktop-e2e` when the required browser/display is available. Update this
guide in the same PR when a named owner, bridge method or trust rule changes.
