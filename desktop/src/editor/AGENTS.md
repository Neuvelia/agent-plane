# Graph editor guidance

The root `AGENTS.md` and parent `desktop/src/AGENTS.md` apply here. This file
adds rules for `desktop/src/editor/`. Read the active F2.1 slice and its ADRs
before changing a document, operation or save flow.

## Find the owner

| Concern | Start here |
| --- | --- |
| Pure draft, history, analysis and run gating state | `doc.ts` |
| Async open/create/edit/validate/save/run orchestration | `useGraphDocument.ts` |
| Typed bridge calls and response mapping | `api.ts`, `types.ts`, `hash.ts` |
| Editor v2 layout: views, banners, inspector slot, drawer and status bar (F2.5f) | `EditorScreen.tsx`, `HeaderBar.tsx`, `SidePanel.tsx`, `IssuesPanel.tsx`, `editor-v2.css` |
| Graph menu, step menu, dock and view controls; menu keyboard and motion | `GraphMenu.tsx`, `StepMenu.tsx`, `CanvasChrome.tsx`, `Menu.tsx` (presence and key model shared with the profile menu: `../ui/presence.ts`, `../ui/menuKeys.ts`) |
| Step cards and edge labels: what a card says about a step | `StepCard.tsx`, `cardModel.ts`, `StepEdge.tsx` |
| "New graph" picker (Blank or a bundled template, F2.12) | `TemplatePicker.tsx`, `../templates.ts` |
| Port handles, operation gestures and keyboard paths | `GraphCanvas.tsx` |
| Display positions and canvas-only helpers | `layout.ts` |
| CodeMirror text editing and key bindings | `YamlEditor.tsx` |
| Node inspector (F2.2): descriptor-driven fields, ports, loop, Advanced | `inspector/Inspector.tsx`, `inspector/model.ts`, `inspector/useEdits.ts` |
| Field labels and help keyed by descriptor id (PL/EN) | `inspector/fieldText.ts` |
| Edge inspector (F2.3 E5b.2): state per requirement 17, value region, Copy | `edgeValue.ts`, `EdgeInspector.tsx`, `EdgeValueBox.tsx`, `valueText.ts` |
| File grants and write safety | `desktop/electron/AGENTS.md`, `internal/api/AGENTS.md` |
| Graph operation semantics and YAML fidelity | `internal/graph/edit/AGENTS.md` |

## Preserve these contracts

- The graph YAML file is the source of truth. The renderer holds a draft and
  the last engine analysis, not a second graph validator or YAML rewriter.
  `doc.ts` is a pure state machine; `useGraphDocument.ts` coordinates the
  bridge and keeps the document alive across screen navigation. New graphs
  start as unsaved schema-v0 text (blank, or a bundled template whose text differs only
  in its `id:` line, F2.12) and are created on the first save.
- Typing marks analysis stale and is validated after a 300 ms pause. A
  validation answer for older text is ignored. A canvas operation sends
  the text on screen with its SHA-256 `base_hash` to `/graphs/edit`; it does
  not write the file. Accept its returned text and analysis as one undo step
  only if the person has not typed or switched documents meanwhile. Keep
  typing coalescing, the shared undo/redo history and its 200-step bound.
- The hash used for an edit identifies the draft text. The `base_hash` sent
  to `/graphs/save` identifies the file bytes last opened or saved; `null`
  requests create-if-absent. Keep these preconditions separate. A refused
  save must not advance `savedText`/`savedHash` or silently overwrite a changed
  file. On focus, a clean draft may reload an outside change; a dirty draft
  presents a conflict. `Keep mine` uses the reported current hash for a
  changed file, `null` to recreate a missing file, or a fresh stat result
  when another file appeared. `Reload` replaces draft and history. Guard
  leaving with unsaved text.
- Canvas controls send versioned operations to the Go API. `can_connect`
  answers whether a target is valid; the UI may display its reason but must
  not duplicate port compatibility, cycle or binding rules. `connections.ts`
  asks the one `list_connections` query; a refused query is shown as a
  notice, never as an empty target list. Selection (node, edge, graph input
  binding) has one owner, `selection.ts`, held by the open editor and shared by
  the canvas, node list and `BindingsPanel.tsx` (F2.3 E5a.2); replace is one
  atomic batch built by `bindings.ts`. `hints.ts` is the one renderer file that
  names validator codes (the two "needs a connection" hints). `layout.ts`
  computes drawing positions and tolerates malformed metadata; moving or
  arranging updates draft `metadata.layout` through `set_layout`, and Save
  persists it without changing the graph's semantic version. When the engine
  reports a parse error, keep the last good graph visible but read-only.
  Anchors, aliases and merge keys also make the canvas read-only while the
  YAML view stays editable.
- `runBlock` reads the engine's diagnostics and current analysis to gate
  “Save and run”; it also requires at least one node. A changed or new draft
  must save successfully before `runGrant` starts. A conflict or other save
  failure starts nothing. The run uses the granted file's saved bytes, never
  unsaved editor text. Do not infer a run result from the start response;
  run state belongs to the event-derived renderer view.
- Keep text, diagnostic and error output as untrusted display data. UI labels
  belong in both PL/EN catalogues; preserve keyboard paths, focus, visible
  read-only reasons and actionable conflict errors. Stories use recorded Go
  API responses alongside controlled failure cases through the in-memory
  bridge.
- Editor v2 (F2.5f, ADR-0050) is presentation only. A step card is a summary read
  from the definition, the descriptors and the diagnostics (`cardModel.ts`); it
  never branches on a node type to decide anything. Controls without engine or
  file-grant support (Test this step, Run from this step, comments, copy and
  duplicate a step, file rename/duplicate/reveal/copy path/delete) stay visible,
  `aria-disabled` and tagged "Soon"; they send nothing. Open, New and Close live in
  the graph menu while a graph is open; node selection by keyboard goes through
  "Find a step".
- The inspector (F2.2) renders the sidecar's descriptors (`node_types`) and
  per-node data (`node_info`); it never branches on a node type or decides a
  value's validity. A committed change (blur, Enter, toggle) is one batch
  through `applyOps`; an invalid value is saved as typed and the engine's
  diagnostic is shown by path. Port renames, retypes and removals are
  previewed with `previewOps` (an edit whose answer is not adopted) and
  confirmed when they touch anything besides the port. `set_loop` only
  creates a loop; every later loop change is one targeted operation
  (`set_loop_attempts`, `set_loop_config`, `set_loop_gate_input`,
  `set_loop_bindings`, ...). Loop binding choices are the engine's answers to
  previewed `set_loop_bindings` batches. Inspector
  fixtures are recorded and pinned by `cmd/agentplane/inspector_fixtures_test.go`;
  `test/fixtures/node-types.json` by `cmd/agentplane/catalogue_test.go`.
- The inspector slot shows the edge inspector (F2.3 E5b.2) when the shared selection is an edge or a graph input
  binding, and the node inspector for a node. The picker hook (`useValuesPicker`) lives in `Open` and is keyed by the
  document's grant, so the run choice lasts until the document closes; `GraphCanvas` only receives it. The state of an
  edge is `edgeValue.ts`, a pure function of the run list, the choice and the values reducer; its order of checks is
  the precedence of requirement 17, so change it only with the spec. A recorded value is user data: it is only ever a
  text node (never markup, a link or an open action; an `artifact_ref` is a plain string), hidden and bidirectional
  characters are shown as `\uXXXX` marks, the formatted JSON view copies literals from the text and is offered only
  when the text parses completely, and the clipboard is written only inside the Copy click, with exactly the
  displayed text. Values are never stored, logged or sent (`edge-inspector.test.tsx` greps the sources for it).

The contracts are `docs/features/F2.1-graph-canvas.md`,
`docs/adr/0020-graph-edit-operations.md`,
`docs/adr/0021-local-graph-document-api-and-write-trust.md`,
`docs/adr/0022-graph-layout-metadata.md`, and `docs/graph-spec.md`. F2.1
slices E1 and E2.1-E2.3 are present; F2.2 (`docs/features/F2.2-node-inspector.md`)
and the F2.4 Issues panel are present; F2.3 E5a.2-E5a.3 (Bindings panel, graph input and output cards, hints) and E5b.2 (edge inspector) are present.

## Choose regression tests

- Edge inspector states, text safety, JSON formatting and Copy: `desktop/test/edge-inspector.test.tsx`; stories `edgeValue.stories.tsx`.
- Draft/history, stale validation and run gating: `desktop/test/editor-doc.test.ts`.
- Editor v2 header, status bar, drawer and step cards: `desktop/test/editor-v2.test.tsx`, `desktop/test/card-model.test.ts`; artboard stories `Design*` in `editor.stories.tsx`.
- Template picker and draft text: `desktop/test/templates.test.tsx`, stories in `picker.stories.tsx`.
- Layout, ID suggestion and deletion counts: `desktop/test/editor-layout.test.ts`.
- Guard against renderer graph rules: `desktop/test/no-graph-rules.test.ts`.
- Inspector model, help catalogue and stories: `desktop/test/inspector-model.test.ts`,
  `desktop/test/inspector-text.test.ts`, `inspector/inspector.stories.tsx`.
- Story states and recorded responses: `editor.stories.tsx`,
  `desktop/test/stories.test.ts`.
- Real grant, edit, keyboard, conflict and Save and run path:
  `desktop/e2e/demo.cjs`.
- Go edit semantics, layout-version stability and write conflicts:
  `internal/graph/edit/edit_test.go`, `internal/api/docs_test.go`.

Run focused Vitest files first, then `npm run check` in `desktop/` and
repository `make check`. For behavior changes to the editor or bridge, run
the relevant real Electron/story checks from `desktop/README.md`. Add a
focused behavior test for a rejected canvas target when changing that path;
the static no-graph-rules test does not prove the refusal UI. Update this
guide when a named owner or contract changes.
