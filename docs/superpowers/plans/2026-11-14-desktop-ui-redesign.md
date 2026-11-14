# Desktop UI Redesign Plan (Tarmind interface -> `desktop/`)

**Status:** plan only. No code is changed by this document.
**Inputs:** the Tarmind interface canvas (11 artboards, "UX-IA-1.0 R1") and the Tarmind design system
(tokens, components, README), compared with what `desktop/` and the local API (`agentplane serve`,
ADR-0019, event schema 9) implement today.
**Scope boundary:** the redesign is a presentation change over existing contracts. Anything that needs a
new API field, event, or graph semantic is listed under "Contract gaps" and needs its own feature spec
(and ADR where AGENTS.md requires one) before implementation.

## 1. Current state

`desktop/` is an Electron shell plus a React renderer (~1,100 lines) with four screens: Runs list, Run
Inspector (React Flow map, node panel with attempts/evidence/log, YAML toggle), Approvals, New run.
Its invariants stay untouched by the redesign:

- node/run/approval state is a pure reduction of the event stream (`src/reducer.ts`); no component stores
  node state;
- the renderer is sandboxed (`contextIsolation`, no `nodeIntegration`, CSP `default-src 'none'`); the
  port/token never leave the main process; the preload exposes only `request`, `subscribe`, `pickGraph`;
- main forwards only `/runs`, `/approvals`, `/graphs`;
- tests rely on `data-testid` hooks (`nav-*`, `run-row`, `approval-card`, `preflight-status`, ...) used by
  the unit tests and `desktop/e2e/demo.cjs`.

Visually it is a first sketch: system font, flat panels, a table, no design tokens.

## 2. Target design (what the artboards define)

| Aspect | Design |
| --- | --- |
| Look | Pure-black ground, diagonal-gradient cards (radius 20), pills, silver primary button, `brand` blue only for focus/links/running, Geist + Geist Mono, sentence case, no emoji |
| Shell | 224px `Sidebar` on wide screens; 52px `IconRail` on canvas screens (editor, run config, inspector); global top bar (environment switcher, search, notifications, account) |
| Status | Layered state vocabulary (Validation, Run, Node, Gate, Approval, Runner, Preflight, Sync, Artifact), always word + colour |
| Data viz | Dots, not bars: `DotChart` (one dot per run), `DotTimeline` (one dot per second), `Heartbeat` |
| Help | Explanations behind `?` HelpTips, not on screen |
| Screens | Dashboard, Project overview + workspaces, Graph editor (Visual/YAML/Split), Run config + preflight, Run inspector, Approvals (inbox + review), Runners, Settings (effective config + privacy), Docs, Hosted mobile approval |

## 3. Gap analysis: design vs. implemented

Legend: **Have** = data/behaviour exists, restyle only. **Derive** = computable in the renderer from
existing data (still a pure function of events/lists). **Gap** = needs a contract that does not exist.

| Design surface | Today | Verdict | Notes |
| --- | --- | --- | --- |
| Dashboard: Active runs | Runs table (`GET /runs`) | Have | status, graph, time; "latest event" needs the stream (see 5.2) |
| Dashboard: Needs attention | Approvals badge only | Derive | pending approvals (`GET /approvals`) + runs with `failed`/`interrupted` status. Runner/degraded items: Gap |
| Dashboard: Outcomes dot chart | none | Derive | last 7 days from `GET /runs` (`status`, `started_at`); "human" dot = run that hit a human node (needs per-run flag, see 5.2) |
| Dashboard: Quick actions | none | Have | "Run graph" and "Open graph (YAML)" only; "New project" / "Connect runner" are Gap |
| Project overview, workspaces | none | Gap | no project/workspace concept in graph spec or API |
| Graph editor: Visual canvas, node inspector, edge mapping | none | Gap | F2.1-F2.4 not implemented; non-goal of F2.5 |
| Graph editor: YAML (read-only) | `<pre>` toggle | Have | restyle + syntax colours + Visual/YAML/Split switch (Split = map + YAML, read-only) |
| Graph editor: Issues panel | validation diagnostics in New run | Have | reuse `POST /graphs/validate` diagnostics (code, message, suggestion, node) |
| Run config: graph, inputs, preflight | path + inputs textarea + Validate | Have (partial) | Steps 1/2/3 (project, version, target) collapse to "graph file"; target is fixed to local sidecar. Preflight rows other than graph validity (runner presence, capabilities, secrets, privacy): Gap |
| Run inspector: live graph, node panel, attempts, evidence, log | implemented | Have | restyle; attempt switch becomes a segmented control |
| Run inspector: KPIs | none | Derive | wall clock (`started_at`..`finished_at`), node time (sum of node `durationMs`), time saved (difference), retries (correction attempts). Tokens: no usage in events, so show "unknown", never 0 (matches the design's own rule) |
| Run inspector: DotTimeline | none | Derive | bucket node intervals per second from event timestamps |
| Run inspector: EventLog | none (only count) | Derive | the reducer already sees every event; keep a bounded tail for display (e.g. last 200), with filters All/Failed/Retried/Human |
| Run inspector: Export, Rerun failed, Cancel run | none | Gap | no cancel endpoint, no resume/rerun-failed (F3.3). Do not render dead buttons |
| Approvals: inbox + review | cards with Approve/Reject + comment | Have | two-pane inbox/review. "Handled" list: derive from stored runs' `approval_decided` events (Derive, needs per-run event read) |
| Approvals: blast radius L1-L3, changed files, diff, "if rejected" routing, nonce | none | Gap | events carry no blast radius (AGENTS.md rule 9 is a later feature). Show prompt + context items from `PendingApproval.context` only |
| Runners (fleet, enroll/revoke, heartbeat, capabilities) | none | Gap, out of scope | cloud/fleet concept (Phase 4). Local variant is possible later: "This device" self-check once an API exposes it |
| Settings: effective configuration, privacy & sync | none | Gap, out of scope | needs runtime-resolved config API; sync is a cloud concern |
| Docs (in-app) | none | Defer | static content; can follow once screens stabilise |
| Hosted mobile approval, accounts, notifications, search | none | Out of scope | hosted web / accounts / cloud are explicitly out of scope until activated (AGENTS.md); local search is optional polish |

Design elements that must **not** be implemented now even though they appear in the canvas: environment
switcher backed by runners, account menu, "Linked to Hosted", sync state pills, remote start, any control
that implies a cloud account. Showing them would misrepresent a local-first product.

## 4. Proposal

### 4.1 Principles

1. **Presentation only.** No scheduling, retry, or state rules in the UI. New numbers are pure functions
   over events or API lists, unit-tested like the reducer.
2. **No dead UI.** A control exists only when a contract backs it. Missing features show nothing, or a
   clearly labelled "not available" state, never a fake action.
3. **Unknown is not zero.** Missing data (tokens, latest event of an unopened run) renders as unknown.
4. **Offline and sandbox-safe.** Fonts and icons are bundled; CSP stays `default-src 'none'` (no Google
   Fonts link as in the artboards).
5. **Keep test seams.** Existing `data-testid` hooks survive; new ones are added per screen.

### 4.2 Information architecture for the desktop app (R1 local subset)

```
Sidebar (224px)            IconRail (52px, canvas screens)
  Dashboard                  Back / Dashboard
  Runs         (list)        Graph map
  Approvals    (badge)       Timeline
  New run                    Log
```

Routes (client state, no URL router needed): `dashboard`, `runs`, `run/:id`, `approvals[/:nodeRunId]`,
`new-run`. "Graphs" appears only as the YAML/Split view inside a run and in New run (no graph library
exists). The top bar carries breadcrumb and a connection indicator for the sidecar (online / connection
lost), which is real state. Search, notifications and account are omitted.

### 4.3 Design-system foundation

- Add `desktop/src/design/tokens.css` generated from the design system's `tokens.json` (colours, type,
  spacing, radii, sizes, shadows); keep it in sync via a small script or a documented manual export. Dark
  theme only, as in the design.
- Bundle Geist and Geist Mono as local font files (see 7, dependency policy), declared with `@font-face`;
  CSP `font-src 'self'` is the only CSP change.
- Component primitives in `desktop/src/ui/` (function components, no UI framework): `Card`, `CardHeader`,
  `Pill` (tones n/b/p/x/w), `Tag`, `Button` (default/primary silver/danger, sizes), `SegmentedControl`,
  `HelpTip`, `Switch`, `KPI`, `DataList`/`ListRow`, `Field`, `CodeView`, `EventLog`, `DotChart`,
  `DotTimeline`, `IconRail`, `Sidebar`, `TopBar`, plus an inline SVG icon set (solid, 24px grid).
- Replace `styles.css` with token-based styles; status vocabulary centralised in `status.tsx` (extend the
  existing `NODE`/`RUN` maps with the design's wording, e.g. "Retrying 2/3", "Human", "Waiting for human").

### 4.4 Screens

1. **App shell.** Sidebar + top bar with breadcrumb and sidecar connection indicator; IconRail variant for
   Run Inspector and New run. Approvals badge keeps `approvals-badge`.
2. **Dashboard.** Cards: *Needs attention* (pending approvals, failed/interrupted runs; one subtitle line,
   HelpTip for the rule), *Active runs* (running runs, link to all runs), *Outcomes* (`DotChart`, 7 days),
   *Quick actions* (Run graph, Open graph). Replaces the plain Runs table; a full runs list stays reachable
   (`nav-runs`, `runs-table`, `run-row` preserved).
3. **Run Inspector.** Header (status pill, graph id, failure kind, live marker, event count). Cards: live
   map (restyled React Flow nodes: 14px radius, status ring, selected = 2px brand ring), KPI row, DotTimeline,
   EventLog with filter, node panel with attempt `SegmentedControl`, reason/evidence `IOBlock`, scope line,
   log. Connection-lost banner and Reconnect behaviour are preserved exactly (F2.5 failure behaviour).
4. **Approvals.** Two-pane: inbox list left, review right (prompt, context rows, comment, Reject (danger) /
   Approve (primary)). After deciding, show the applied state only when the `approval_decided` event arrives
   (current behaviour). 409 handling unchanged.
5. **New run.** Steps: graph file (choose/validate), inputs, preflight (`CheckRow`s from validation
   diagnostics, `Ready`/`Warnings`/`Blocked`), Start run (silver). Keep `graph-path`, `validate`,
   `preflight-status`, `run` hooks. Inputs stay `key=value` lines in this iteration; structured fields are a
   later refinement.
6. **YAML view.** `CodeView` with token colours and line numbers; Visual / YAML / Split segmented switch
   inside Run Inspector (Split = map beside YAML, read-only).

### 4.5 Phasing (each phase is shippable and keeps `npm run check` and `make desktop-e2e` green)

| Phase | Content | Depends on contracts? |
| --- | --- | --- |
| R0 Foundation | tokens, fonts, primitives, shell (Sidebar/TopBar/IconRail), status vocabulary; existing screens re-hosted unchanged in the new shell | none |
| R1 Re-skin | Run Inspector, Approvals, New run restyled with primitives; node/edge styling; YAML `CodeView` | none |
| R2 Derived insight | KPIs, DotTimeline, EventLog, Dashboard (Needs attention, Active runs, Outcomes), Handled approvals | pure selectors over events/lists; needs 5.2 if per-run summaries are required |
| R3 Polish | keyboard navigation, `prefers-reduced-motion`, focus rings per spec, empty/error states, HelpTips copy, docs link stub | none |
| Later (separate specs) | graph editing (F2.1-F2.4), attempt compare (F2.8), onboarding/capability detection (F2.11), runners/settings/project concepts, hosted/mobile | each needs its own spec/ADR |

## 5. Contract gaps (not part of this redesign; propose as separate features)

### 5.1 Missing endpoints/events the design assumes

| Need | Why | Suggested home |
| --- | --- | --- |
| Cancel run / rerun failed | inspector header actions | new feature spec; rerun depends on F3.3 resume |
| List graphs in a workspace | Graph library, Quick actions "Open graph" | F2.1 or a small F2.5 follow-up |
| Blast radius on human gates | L1-L3 pill, review ordering | later human-gate feature (AGENTS.md rule 9) |
| Token/usage reporting | Tokens KPI | adapter progress payload (F1.3 follow-up); until then "unknown" |
| Handled approvals list | "Handled" section | derive from run events, or add `GET /approvals?state=all` |
| Project / workspace / runner / settings / sync | Project, Runners, Settings artboards | Phase 4 cloud/fleet concepts; not for the local core |

### 5.2 Run list enrichment (optional, small)

`GET /runs` returns `run_id, graph_id, started_at, status, event_count`. Dashboard "latest event", node
progress (`2/5`) and a "had human gate" dot need either opening each run's stream (expensive) or extending
the summary with `finished_at`, `node_count`, `last_event_type`, `waited_for_human`. This is an additive
API change to ADR-0019 and should be a tiny spec/ADR amendment. R2 can ship without it by showing only what
`GET /runs` provides.

## 6. Testing and acceptance

- **Reducer/selectors:** unit tests for every new derived value (wall clock, node time, saved time,
  retries, per-second buckets, outcomes by day, attention list). A fixed event log must produce identical
  derived output from replay and from live apply, as for the reducer today.
- **Unknown handling:** tests that missing durations/usage render as unknown, never 0.
- **Component tests (Vitest, no new framework unless justified):** status vocabulary mapping, DotChart
  slot counts, EventLog filtering and bound.
- **E2E:** extend `desktop/e2e/demo.cjs` under xvfb: new shell navigation, Dashboard needs-attention shows
  the pending approval, inspector KPIs/timeline present, approve/reject via the new review pane, restart
  shows the stored run.
- **Security regression:** assert CSP remains `default-src 'none'` apart from `font-src 'self'`/bundled
  assets, no external requests (fonts bundled), preload surface unchanged, forwarded path allowlist
  unchanged.
- **Accessibility:** contrast per tokens (design documents 7.3:1 `muted` on `bg`), visible focus ring on
  every control, real `button`/`input` elements, `aria-label` on icon-only buttons, status never colour-only.
- **Manual:** update `docs/validation/F2.5-desktop-demo.md` with a visual checklist per screen.

Acceptance for the redesign (measurable): all current e2e and unit tests pass; every screen in 4.4 renders
only from existing API data; no new endpoint, event or preload method is added in R0-R3; no network request
leaves the app (fonts included); screenshots of Dashboard, Run Inspector (running, waiting for human,
failed, connection lost), Approvals and New run (ready, blocked) are attached to the PR for review.

## 7. Risks, ambiguities, decisions needed

- **Fonts/dependencies.** Geist is not a system font and CSP forbids the Google Fonts link used in the
  artboards. Decision: vendor the font files (OFL licence) into `desktop/`, or use a dev-only package such
  as `@fontsource/geist`. Per the dependency policy this must be explained in the implementing PR
  (runtime asset, no network, no security impact). Fallback stack stays `ui-sans-serif, system-ui`.
- **Tokens source of truth.** The design system lives in an artifact; the repo needs a committed copy of
  `tokens.json` (or the generated `tokens.css`) so the build does not depend on it.
- **Design vs. product scope.** The canvas is cloud-first in places (hosted, accounts, runner fleet). The
  plan implements the local subset only. Confirm that Projects, Runners, Settings and Docs are deferred.
- **Graph editor.** The most visible artboard is a feature (F2.1-F2.4), not a restyle. Confirm it should be
  specified separately, next after R1/R2.
- **Derived metrics correctness.** "Node time" and "saved time" are renderer calculations; they must be
  labelled as computed from events and unit-tested, and must not be presented as runtime-authoritative.
- **Performance.** Event log and timeline over very long runs need bounded buffers (reducer already caps
  node logs at 64 KiB); the EventLog tail and DotTimeline must be bounded the same way.
- **React Flow styling.** Custom node components must keep node ids and `data-testid`s used by e2e.
- **Language.** The canvas has Polish labels in one board (`Main`); product UI copy is English per the
  design system README. Confirm English-only for now.
- **Roadmap hygiene.** Per AGENTS.md, a non-trivial behaviour change needs a feature spec. Before R0 starts,
  add `docs/features/F2.5a-desktop-redesign.md` (from `_template.md`) containing section 6's acceptance
  criteria, and update `desktop/README.md` "Not implemented" once screens land.

## 8. Immediate next steps

1. Review and confirm the decisions in section 7 (fonts, scope deferrals, English copy).
2. Write `docs/features/F2.5a-desktop-redesign.md` from this plan.
3. Implement R0 on its own branch/PR, then R1, R2, R3 as separate PRs.
