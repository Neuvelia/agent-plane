# Renderer guidance

The root `AGENTS.md` applies here. This file adds navigation and contract
rules for `desktop/src/`. Read `desktop/README.md` and the active feature
spec before changing a screen, API wrapper or event view. The sibling
`desktop/electron/AGENTS.md` owns main/preload trust rules.

## Find the owner

| Concern | Start here |
| --- | --- |
| Bridge types and thin API calls | `types.ts`, `api.ts`; editor calls in `editor/api.ts` |
| Event-derived node/run/approval detail | `reducer.ts` |
| Attempt compare (F2.8a): pure model and view | `compare.ts`, `AttemptCompare.tsx`, `check.tsx` |
| List polling, SSE replay/follow and reconnect | `hooks.ts` |
| Navigation, shared editor lifetime and connection badge | `App.tsx`, `connection.tsx` |
| Runs, inspector and approvals screens | `views.tsx`, `GraphMap.tsx`, `NodePanel.tsx` |
| New run with preflight (F2.5e): view, state, pure preflight model | `NewRun.tsx`, `useNewRun.ts`, `newRunSession.ts` (pick/grant ordering), `settledPick.ts` (arrow-key picks), `newRunModel.ts`, `preflight.ts` |
| Inbox triage queue (F2.5b): queue, detail, decision in place | `inbox.ts`, `inboxRun.ts`, `inboxView.tsx`, `InboxDetail.tsx`, `ui/glyphs.tsx` |
| Sidebar (F2.5b): New run, navigation, recent runs, Later, profile | `ui/shell.tsx`, `sidebarRecent.tsx`, `ui/icons.tsx` |
| Event-derived run insight | `insight.ts` |
| Graph draft, canvas and YAML presentation | `editor/doc.ts`, `editor/useGraphDocument.ts`, `editor/GraphCanvas.tsx` |
| Shared controls, design and language | `ui/`, `design/`, `i18n/` |
| Settings: language and the opt-in "Record run values" switch | `settings.tsx` (preference held by Electron main) |
| Profile menu, Keyboard shortcuts and About (F2.5c) | `ui/profileMenu.tsx`, `ui/menuKeys.ts`, `ui/presence.ts`, `ui/dialogs.tsx` |
| Theme choice (System/Light/Dark) and `data-theme` (F2.5c, ADR-0047) | `theme.tsx`; light tokens in `design/tokens.json` |
| Recorded run values and "Show values from run" (F2.3 E5b.1) | `values.ts`, `valuePicker.ts`, `useRunValues.ts`, `ValuePickerView.tsx` |
| Edge inspector: states, safe value text, Copy, session recording hint (F2.3 E5b.2) | `editor/edgeValue.ts`, `editor/EdgeInspector.tsx`, `editor/EdgeValueBox.tsx`, `editor/valueText.ts`, `useSessionRecording.ts` |
| Welcome screen as four-step setup (F2.11, ADR-0048): screen and capability hook, pure step/card/verdict/timing rules, check step, step panels, icons, Create outcome, styles | `welcome.tsx`, `setup.ts`, `setupCheck.tsx`, `setupSteps.tsx`, `setupIcons.tsx`, `setupCreate.ts`, `setup.css`; capability report and first-run flag in `capabilities.ts` |
| New graph picker, bundled templates, draft and notices (F2.12) | `editor/TemplatePicker.tsx`, `templates.ts` |
| Command palette and global shortcuts, one listener for every app chord (F2.5d) | `palette.ts` (pure model), `ui/CommandPalette.tsx`, `useShortcuts.ts`, wiring in `App.tsx` |
| Story fixtures and bridge substitute | `screens.stories.tsx`, `storybook/bridge.tsx`, `storybook/fixtures.ts` |

## Preserve these contracts

- The renderer reaches the Go sidecar through `window.agentplane`, never a
  direct network, Node or filesystem API. Electron main owns the token and
  allowed routes. Keep `api.ts` and `editor/api.ts` as thin bridge wrappers;
  coordinate any bridge or API shape change with their owning packages.
- `reducer.ts` is the source of detailed run and node state. Apply events in
  sequence order, ignore replayed sequence numbers, preserve the input state
  and reconstruct the same view from stored and live events. The API graph
  summary supplies node IDs and dependencies; `GraphMap.tsx` derives their
  display positions. Events supply status, attempts, checks and approval
  facts. Do not infer execution transitions from timers or HTTP decision
  responses. The approvals inbox and run list are API lists.
- `useRunStream` may batch rendering but must apply every event in order.
  Only the server's explicit `end` completes a stream. If it is lost while
  the run is still active, keep the last reduced state and offer reconnect;
  a loss after `run_finished` does not reopen a completed run. A nonempty,
  normally ended log without a terminal run event is interrupted, and so is
  every node that started and never finished, unless the store reports the run
  as `running` (a writer in another process holds it); then `liveElsewhere.ts`
  re-reads only the run's status every few seconds and re-subscribes when it
  changes, and nothing is settled before the store has answered. `usePoll`
  is for lists, and a failed list read is an error with Retry, never an empty
  list or stale data shown as current. Preflight displays the engine's
  diagnostics rather than validating graph semantics in TypeScript.
- Inbox and inspector insight are pure views of reduced events or API
  lists. Preserve bounded log/event rows and timeline columns. Do not
  fabricate missing durations or token usage; show unknown where a KPI
  cannot be computed. UI state can hold selection, navigation and draft text;
  it must not become a second scheduler or authority for run status.
- The editor holds a local YAML draft, history and conflict state in
  `editor/doc.ts`/`editor/useGraphDocument.ts`; that is distinct from run
  state. The sidecar validates text, applies canvas operations and decides
  `can_connect`; `editor/layout.ts` supplies visual positions, not graph
  validity. A canvas edit uses the engine's returned text. Treat graph text,
  diagnostics and logs as untrusted display data.
- Put UI text in both `i18n/en.ts` and `i18n/pl.ts`; keep event types, IDs,
  paths, commands, YAML, diagnostic codes and engine messages as received.
  Use generated `design/tokens.css` and the documented overrides. Keep
  keyboard access, visible focus, non-colour status cues and reduced-motion
  behavior. Stories use recorded events/API fixtures through the in-memory
  bridge, not hand-built run state or a live sidecar.

Read `docs/features/F2.5-live-map.md`,
`docs/features/F2.5a-desktop-redesign.md`,
`docs/features/F2.1-graph-canvas.md`,
`docs/adr/0019-local-run-store-and-loopback-api.md`,
`docs/adr/0020-graph-edit-operations.md`, and
`docs/adr/0021-local-graph-document-api-and-write-trust.md` for the relevant
contracts. F2.1 is delivered in slices; do not treat future editor behavior
as implemented merely because the full spec describes it.

## Choose regression tests

- Event reduction, replay and stream loss: `desktop/test/reducer.test.ts`;
  schema-10 `value_recorded` streams leave run views unchanged:
  `desktop/test/schema10.test.ts`.
- Settings recording switch: `desktop/test/recording.test.tsx`.
- Run overview selectors and views (F2.6: waits, critical path, outcome, node filters, timeline segments): `desktop/test/overview.test.ts`, `desktop/test/overview.test.tsx`.
- Recorded values (F2.3 E5b.1: `values.ts` reducer, `valuePicker.ts` picker rules, `useRunValues.ts`, `ValuePickerView.tsx`; values stay out of `reducer.ts` and never touch storage or console): `desktop/test/edge-values.test.ts`, `desktop/test/edge-values-view.test.tsx`.
- Edge inspector (F2.3 E5b.2: the ten states and their precedence, `\uXXXX` marks for control and bidirectional characters, JSON formatting that never changes a literal, Copy only on click, text only, no link or storage): `desktop/test/edge-inspector.test.tsx`, stories `editor/edgeValue.stories.tsx`.
- Welcome screen, capability text and first-run flag: `desktop/test/capabilities.test.tsx`; the four-step setup (verdict, cards, draft, motion budget, CSS layering) in `desktop/test/setup.test.tsx`.
- Template picker, draft (only the `id` line changes), notices and EN/PL keys: `desktop/test/templates.test.tsx`; no YAML parsing or graph rule in the picker: `desktop/test/no-graph-rules.test.ts`.
- Attempt compare model and view: `desktop/test/compare.test.tsx`.
- Command palette (F2.5d: filtering, `>`/`#` scopes, Tab, active row, shortcuts, dialog/combobox markup, states): `desktop/test/palette.test.ts`, `desktop/test/palette-view.test.tsx`, DOM behaviour under jsdom in `desktop/test/palette-dom.test.tsx` (the file opts in with `// @vitest-environment jsdom`; jsdom has no `<dialog>` methods, so the test stubs them), stories `ui/palette.stories.tsx`.
- API/list/preflight failures: `desktop/test/api.test.ts`,
  `desktop/test/preflight.test.ts`; New run inputs, preflight rows and headline over
  recorded engine answers: `desktop/test/new-run.test.tsx`; pick and grant ordering:
  `desktop/test/new-run-session.test.ts`; stories `NewRun.stories.tsx`.
- Inbox queue and detail selectors (F2.5b): `desktop/test/inbox.test.ts`; stories `Screens/Inbox*`.
- Timeline and bounded event detail:
  `desktop/test/insight.test.ts`.
- Editor draft/layout and absence of local graph rules: `desktop/test/editor-doc.test.ts`,
  `desktop/test/editor-layout.test.ts`, `desktop/test/no-graph-rules.test.ts`.
- For rejected canvas connections, inspect `editor/GraphCanvas.tsx` and
  `editor/editor.stories.tsx`; add a focused behavior test when changing that
  path. The static no-graph-rules test does not test refusal UI behavior.
- Profile menu, theme and language preferences, shortcuts and About (F2.5c):
  `desktop/test/profile-menu.test.tsx`, `desktop/test/shell-dialogs.test.tsx`;
  the menu's keyboard model runs in a real browser in `npm run stories:test`.
- Language, design, accessibility and stories: `desktop/test/i18n.test.ts`,
  `desktop/test/tokens.test.ts`, `desktop/test/contrast.test.ts`,
  `desktop/test/a11y-static.test.ts`, `desktop/test/stories.test.ts`.

Run focused Vitest files first, then `npm run check` in `desktop/` and
repository `make check`. For changed desktop behavior, follow the real
Electron/story checks in `desktop/README.md` when relevant. Update this guide
when an owner or contract changes.
