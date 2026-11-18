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
| Toolbar, YAML/Visual/Split views, diagnostics and conflict UI | `EditorScreen.tsx` |
| "New graph" picker (Blank or a bundled template, F2.12) | `TemplatePicker.tsx`, `../templates.ts` |
| Port handles, operation gestures and keyboard paths | `GraphCanvas.tsx` |
| Display positions and canvas-only helpers | `layout.ts` |
| CodeMirror text editing and key bindings | `YamlEditor.tsx` |
| Node inspector (F2.2): descriptor-driven fields, ports, loop, Advanced | `inspector/Inspector.tsx`, `inspector/model.ts`, `inspector/useEdits.ts` |
| Field labels and help keyed by descriptor id (PL/EN) | `inspector/fieldText.ts` |
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
  not duplicate port compatibility, cycle or binding rules. `connect.ts`
  asks it for every candidate input in batches of at most `MAX_OPS_PER_BATCH`
  (the engine's `MaxOps`, kept equal by a test); a refused batch is shown as a
  notice, never as an empty target list. `layout.ts`
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

The contracts are `docs/features/F2.1-graph-canvas.md`,
`docs/adr/0020-graph-edit-operations.md`,
`docs/adr/0021-local-graph-document-api-and-write-trust.md`,
`docs/adr/0022-graph-layout-metadata.md`, and `docs/graph-spec.md`. F2.1
slices E1 and E2.1-E2.3 are present; F2.2 (`docs/features/F2.2-node-inspector.md`)
and the F2.4 Issues panel are present; F2.3 remains a separate scope.

## Choose regression tests

- Draft/history, stale validation and run gating: `desktop/test/editor-doc.test.ts`.
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
