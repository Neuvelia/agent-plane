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
npm run check                # typecheck + reducer tests + production build
make desktop-e2e             # real Electron + real sidecar under xvfb
```

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

Screens implemented from the Tarmind interface design: Runs (IA-01, list only),
Run Inspector (IA-15/16) with live map, attempts, reason/evidence and log,
Approvals (IA-17/18), New run with preflight (IA-12/13) and the YAML view of the
graph (IA-07-11, read-only). Not implemented: Project, Runners, Settings, mobile
approval, docs, onboarding, attempt compare, graph editing.
