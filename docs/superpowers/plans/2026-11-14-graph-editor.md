# Graph Editor Plan (F2.1 Graph Canvas, F2.2 Node Inspector, F2.3 Edge/Data Mapping, F2.4 Validation UI)

**Status:** plan only. No code is changed by this document. Nothing here is an executable contract:
each slice needs its own feature spec (`docs/features/_template.md`) and, where marked, an ADR before
implementation (AGENTS.md).
**Inputs:** AGENTS.md, `docs/graph-spec.md`, `docs/domain-model.md`, `docs/security-model.md`,
`docs/roadmap.md` and `docs/reference/full-roadmap-features.md` (F2.1-F2.4), `internal/graph`,
`internal/api`, `cmd/agentplane/serve.go`, the merged desktop (F2.5, F2.5a) and the Tarmind
"Graph Editor (Split)" artboard.

## 1. Current state

What exists and matters for editing:

| Area | Fact (verified in the repo) | Consequence for the editor |
| --- | --- | --- |
| Graph file | One YAML document, strict parse (unknown fields rejected), `schema_version: 0`. `graph.Marshal` normalises formatting: Go maps make `nodes` come out alphabetical, comments and blank lines are lost, flow style is rewritten. | A naive "parse, edit, marshal" save would rewrite `.agents/graphs/demo-vertical.yaml` (4 comment lines, deliberate node order). Graphs live in Git; a UI save must not churn them. **This is the central risk.** |
| Semantic version | `graph.SemanticVersion` hashes the definition with `name` and `metadata` removed, `edges`/`graph_input_bindings`/`units` order-normalised; a test already proves metadata and edge order do not change it. | Layout can live in `metadata` without changing the run version. |
| Validation | `graph.Validate(def)` is semantic only: refs, types, cycles, required inputs, unused-output warning. Diagnostics carry `path`, `node_id`, `edge_index`, `graph_input_binding_index`, no source line. | Issues can focus a node or edge by id/index. No line numbers, so "go to YAML line" needs a locator we do not have yet. |
| Node-config validation | Shell, gate and agent config is validated by each executor's `ValidateNodeConfig`, called by the runtime at run start (human nodes are already checked inside `graph.Validate`). `serve`'s `validateForAPI` and `agentplane validate` call only `graph.Validate`, so shell, gate and agent config errors surface only at run start. | Today the UI "Validate" misses config errors until the run starts. F2.2's "an invalid config cannot be saved without a visible error" needs the config validators exposed through the same side-effect-free path. |
| API | `POST /graphs/validate {path}` reads a file by path. No endpoint reads the typed definition, validates unsaved content, lists graphs or writes anything. Request bodies are capped at 64 KiB. | New read, validate-content and save endpoints are required (a durable local API contract: ADR). |
| Desktop trust boundary | The renderer is sandboxed; the preload exposes `request`, `subscribe`, `pickGraph`; main forwards only `/runs`, `/approvals`, `/graphs`. `graph_path` for runs and validation is a string the renderer supplies. | Reading by renderer-supplied path is tolerable. **Writing** by renderer-supplied path is not (see 4.2). |
| Desktop graph view | Read-only React Flow map from `GraphSummary` (id, type, max_attempts; edges as from/to), dagre auto layout, YAML shown as text. No ports, no config. | The editor needs the full typed definition (ports, config, loop) over the API, not the summary. |
| Layout | `metadata` is an open mapping excluded from the semantic version; domain-model invariant 7: canvas metadata must not change execution semantics. | `metadata.layout` is allowed by the contract; storage location is a decision (4.3). |

### Design artboard versus the real v0 contract

The Tarmind artboard is a design target, not a spec. Several things on it do not exist in schema v0 and
**must not be implemented as drawn**:

| Artboard | Reality |
| --- | --- |
| `foreach: {{t1}}` and `depends_on: [plan]` in the YAML | Not in v0 (strict parser rejects them). `depends_on` is an ordering-only edge, forbidden by AGENTS.md rule 4. Parallel fan-out is "explicit static units" (F1.7), a different mechanism. |
| Agent inspector: adapter select, `timeout`, `permissions: network off / shell on` | v0 agent config is `adapter`, `workspace`, one of `task`/a `task` input, `allowed_paths`, `context_refs`, `environment_refs`. No timeout or permission model yet (F3.x). |
| Loop block "Max attempts / Timeout / Check: pytest tests/backend" | v0 loop is `max_attempts` 1-10, an embedded Gate (`command`, `json_predicate`, `reported_file_scope`) and explicit `bindings`. |
| Warning W102 "target does not consume source output", "unused" badge | The validator's real warning is "declared output not consumed". Codes and wording differ; the UI must show the engine's own diagnostics. |
| Header "Saved · v12", "Run v12" | There is no version counter. A version is `sha256-v0:<hash>`; show a short hash. No version history exists or is planned here. |

Recommendation: keep the artboard's layout (canvas, inspector, issues panel, Visual/YAML/Split) and
generate every field from the real v0 contracts; send the divergences above back to design.

## 2. Scope interpretation

In scope (a local, no-account graph editor over the YAML file, desktop only):

- **F2.1 Graph Canvas:** open/create a graph file; add Shell, Agent, Gate and Human nodes; move; delete;
  connect an output port to an input port only when types are compatible; zoom/pan; layout stored as
  non-semantic metadata; Visual / YAML / Split where YAML and Split are editable.
- **F2.2 Node Inspector:** type-specific forms for the four node types and the `loop` block, sections
  inputs / execution / loop and gate / outputs; inline validation from the engine; a preview of the
  resolved inputs where the graph alone determines them.
- **F2.3 Edge/Data Mapping:** an edge list and a binding editor (source output to target input, type
  shown), graph input bindings, the "unused output" warning with a suggestion, and the missing-binding
  hint.
- **F2.4 Graph Validation UI:** issues panel with severity filter; clicking an issue focuses the node or
  edge; a badge on the offending element; Run disabled only while blocking errors exist; every CLI
  diagnostic has a UI counterpart.

Explicitly out of scope: templates (F2.12), auto-layout beyond the existing deterministic dagre fallback,
visual groups, a graph library or project concept (Projects stay deferred), version history, collaboration,
cloud or Hosted editing, new node types or schema fields (any schema change is its own spec and ADR),
permissions and timeouts, run-value inspection on edges (see 8, question 8), LLM-assisted editing.

## 3. Principles for the whole feature

1. **The file is the truth.** Canvas, forms and YAML are three views of one document. The renderer holds a
   draft of the *text*; it never holds a second model of the graph.
2. **Go owns graph semantics.** The renderer sends edits; the sidecar parses, applies, validates and
   returns text, the typed definition and diagnostics. No graph validation logic is written in TypeScript,
   so the editor cannot disagree with `agentplane run`.
3. **No silent reformatting.** Saving after a canvas edit changes only what the edit changed. Raw YAML edits
   are saved byte for byte. Formatting the whole file is an explicit, separate action.
4. **Every save proves itself.** After applying an edit the server re-parses the result and checks that its
   semantic version equals the one expected from the edit (and, for layout-only edits, equals the version
   before). Otherwise it refuses and reports an error; nothing is written.
5. **Writes are capabilities.** The renderer cannot write to a path it names (4.2).
6. **Everything reachable by mouse is reachable by keyboard.** Drag-and-drop is an accelerator; adding,
   connecting and deleting also work from toolbar buttons and the inspector/edge forms.
7. **Deterministic first.** No LLM anywhere in editing; templates and suggestions are deterministic.
8. **Localised and tested like R0-R3:** PL/EN catalogues, stories per state over recorded fixtures, axe on
   every story, e2e on the real Electron app.

## 4. Proposed approach

### 4.1 Edit operations on the YAML tree (ADR required)

Options for how a canvas/form edit becomes file changes:

| Option | Fidelity | Cost | Verdict |
| --- | --- | --- | --- |
| A. Parse to `Definition`, mutate, `Marshal` | Loses comments, reorders nodes, rewrites style | Lowest | Rejected: churns Git diffs and destroys author comments |
| B. **Typed edit operations applied to the `yaml.Node` tree** (add/remove node, set config field, add/remove edge, set port, move node...), then encode | Keeps comments and key order; whitespace normalised to 2 spaces; blank lines and exotic flow style may change | Medium (new `internal/graph/edit` package, ops table, fidelity tests) | **Recommended** |
| C. Text-first: canvas only inserts text into an editor | Perfect for what is not touched | Fragile to generate, no structural guarantee | Rejected as the primary path; raw text editing remains available as a view |

The ADR must record the measured guarantee. A **spike (E0)** applies representative operations to the
existing graphs (`demo-vertical`, `hello-parallel`, `phase0-cli`) and reports exactly what is preserved
(comments, key order, quoting, anchors, flow style). The ADR states the promise (for example: comments and
key order are preserved, indentation is normalised) and the refusals (anchors/aliases/merge keys are not
patched: the editor opens such a file read-only for canvas edits and says why).

The operations are an API contract (JSON, versioned), small and total: each op either applies exactly or
fails with a structured error naming the node, edge or path, as AGENTS.md error rules require.

### 4.2 Local graph document API and the write trust model (ADR required)

New endpoints on the existing loopback, bearer-token server (no new listener, same Host check, same body
limits; the 64 KiB cap is raised for graph documents to 1 MiB with a documented reason):

| Endpoint | Purpose |
| --- | --- |
| `POST /graphs/open {grant}` | Source text, source hash (sha256 of bytes), typed definition DTO (explicit JSON tags, separate from runtime types), semantic version, diagnostics (graph and node-config), and per-type field descriptors in use. |
| `POST /graphs/edit {grant, base_hash, ops}` | Apply ops to the stored text, return the new text, definition, version and diagnostics. Does not write. |
| `POST /graphs/validate {text}` (extends today's `{path}`) | Validate unsaved text, including node-config validation. Same result shape as the CLI. |
| `PUT /graphs/save {grant, base_hash, text}` | Atomic write (temp file in the same directory, fsync, rename), refused with 409 if the file changed since `base_hash`. |

**Write trust model.** Today a renderer-supplied `graph_path` string is accepted for reading and running.
For writing that is a hole: a compromised renderer could overwrite any `*.yml` the user can write
(for example a CI workflow). Proposed: the **main process owns file choice**.

- New preload methods `openGraph()` and `newGraph()` run native dialogs in main. Main registers the chosen
  absolute path under an opaque `grant` id with the sidecar (`POST /graphs/grants`, a path main calls
  directly and never forwards from the renderer) and returns only the grant to the renderer.
- The renderer addresses a graph by grant, never by path. The sidecar rejects any read/write whose grant it
  does not know.
- The sidecar still enforces: extension `.yaml`/`.yml`, regular file or a new file in an existing
  directory, no symlink in the final component, size cap, atomic rename, mode preserved, and `base_hash`
  required for overwrite.
- Running keeps working as today (path or grant); Run always uses the **saved** file, never unsaved editor
  state (the editor offers "Save and run").

This changes the preload surface that F2.5/F2.5a froze, so it needs the ADR and an update to the
"renderer reaches the API only through `request`, `subscribe`, `pickGraph`" rule in `desktop/README.md`
and F2.5.

### 4.3 Layout storage (ADR required, small)

| Option | Pros | Cons |
| --- | --- | --- |
| **Inline `metadata.layout.nodes.<id>: {x, y}`** (recommended) | Single file; matches the spec text "layout is metadata"; already excluded from the semantic version; travels with the graph | Moving a node produces diff lines (mitigated: integers, stable key order, written only when the user moves a node) |
| Sidecar `graph.layout.json` | Keeps YAML diffs clean | Two files to keep together; easy to lose; second write path |

With no layout present the existing deterministic dagre layout is used and nothing is written until the user
moves a node. A moved node never changes the semantic version (a test enforces it, in addition to the
existing metadata test).

### 4.4 Node forms come from one source of truth

The four node types' config is validated by Go executors. To avoid hand-copying that contract into forms
that then drift, the server returns **field descriptors** per node type (key, type, required, enum, help
key, nested shape for the gate kinds and the loop) generated from the same Go definitions the executors use,
plus the executors' own `ValidateNodeConfig` result for the current values. Forms render descriptors;
unknown config keys are preserved and shown in an "Advanced" key/value view, never dropped.

Alternative (hand-written TS forms with contract tests against the Go validator on fixtures) is cheaper
now and drifts later; listed as a decision (8, question 4).

### 4.5 Renderer architecture

- State: `draftText` (what is on screen), `savedHash`, `dirty`, `history` (a stack of texts for
  undo/redo), `result` (last definition DTO and diagnostics from the server). Everything else is derived.
- Each canvas/form action builds an op, calls `/graphs/edit`, and replaces the draft with the returned text.
  Raw YAML edits are debounced into `/graphs/validate`.
- Canvas: the existing `@xyflow/react` and `dagre` (no new dependency). Nodes get one handle per declared
  port; `onConnect` is allowed only when the server says the types are compatible (the compatibility rule
  is Go's, exposed as a check op, not re-implemented). Node objects stay static (R3 lesson) with state
  passed by context.
- Conflict handling: on window focus compare the file hash; clean draft reloads silently, dirty draft shows
  a keep-mine / reload banner. Save with a stale hash gets 409 and the same banner.
- YAML view: a plain `<textarea>` with line numbers first (no new dependency); a code-editor dependency
  (CodeMirror 6) is a decision for the slice that adds Split editing (8, question 5).
- Inspector, issues panel and edge list are cards on the existing design system and Storybook.

## 5. Delivery slices

Roadmap IDs are kept; the order differs from the roadmap's strict dependency column on purpose (F2.4's
feedback is cheap once the API exists and de-risks everything after it).

| Slice | Content | Needs |
| --- | --- | --- |
| **E0 Spike + ADRs** | Fidelity spike on the repo graphs; ADR-A edit operations, ADR-B graph API and write trust, ADR-C layout; specs F2.1-F2.4 drafted from the template | Decisions in section 8 |
| **E1 Graph document API** (Go) | `internal/graph/edit` ops engine, typed DTO, field descriptors, node-config validation on the validate path, grants, open/edit/validate/save endpoints, preload `openGraph`/`newGraph` | E0 |
| **E2 F2.1 Canvas** | Open/new/save, Visual/YAML (editable)/Split, add/move/delete/connect, layout metadata, undo/redo, dirty/conflict handling, Save and run | E1 |
| **E3 F2.4 Validation UI** | Issues panel, filter, click to focus, badges, Run gating, CLI-parity test | E2 |
| **E4 F2.2 Inspector** | Forms for shell/agent/gate/human and loop+gate, inline validation, resolved-input preview, Advanced view | E2, E3 |
| **E5 F2.3 Mapping** | Edge list/binding editor, graph input bindings, unused-output warning and suggestion, keyboard-only connect | E2-E4 |

Each slice is its own branch and PR with a spec section, PL/EN strings, stories (including loading,
error, conflict and read-only states), e2e, and screenshots, as in F2.5a.

## 6. Files (indicative; the specs fix them)

New: `docs/features/F2.1-graph-canvas.md` ... `F2.4-graph-validation-ui.md`, ADRs for 4.1-4.3,
`internal/graph/edit/` (ops, tests), `internal/api/graphs.go` (document endpoints and grants),
DTO/descriptor code next to the executors, `desktop/src/editor/` (state, canvas, inspector, issues, edge
list), editor stories and fixtures.

Changed: `internal/api/server.go` (routes, body limit for graph documents), `cmd/agentplane/serve.go`
(wire validation including config), `desktop/electron/main.cjs` and `preload.cjs` (grants, dialogs; forward
list), `desktop/src/GraphMap.tsx` (shared canvas pieces), `desktop/src/i18n/*`, `desktop/README.md`,
`docs/graph-spec.md` (the `metadata.layout` shape), `docs/features/README.md`.

## 7. Tests (what must be proven, not how)

- **Go, edit ops:** every op applies and fails as specified (table tests); the result re-parses; semantic
  version equals the expected one; layout ops never change it; ops are idempotent where stated.
- **Go, fidelity:** on the repo graphs, edits preserve comments and key order exactly as the ADR promises;
  aliases/merge keys are refused, not corrupted.
- **Go, parity:** the API's diagnostics equal `agentplane validate` for the same text, including config
  errors (every CLI diagnostic has an API counterpart).
- **Go, security:** save refuses unknown grants, non-YAML names, directories, symlink final components,
  oversize bodies, stale `base_hash` (409) and leaves the old file intact on any failure (atomic write);
  grants cannot be created through the forwarded renderer path.
- **Renderer unit:** history/undo stack, dirty/conflict state machine, op builders; no graph rules.
- **e2e (real Electron + sidecar):** create a graph from scratch in the UI, save, run it with the CLI
  unchanged (the roadmap criterion); move a node and see a `metadata`-only diff with the same version;
  external edit on disk triggers the conflict flow; an invalid config shows a visible error and Run is
  disabled only for blocking errors; keyboard-only path adds two nodes, connects them and saves.
- **Stories + axe:** canvas, inspector sections, issues panel, edge list, conflict banner, read-only
  (alias) notice, loading/error, in EN and PL.

## 8. Risks and decisions needed

Risks:

1. **YAML fidelity** (4.1): mitigated by the spike gate, the "re-parse and compare version" safety net, and
   refusing unpatchable files rather than guessing.
2. **Write abuse** (4.2): mitigated by grants owned by main plus sidecar-side checks; this is a trust
   boundary change and must be reviewed as one.
3. **Config drift** between forms and executors (4.4): mitigated by server descriptors or contract tests.
4. **Keyboard and screen-reader access to a canvas** is hard; the principle is "every action has a
   non-drag path", verified by an e2e that never uses the pointer.
5. **Large or hand-written graphs** the editor cannot patch (aliases, merge keys, `config` with exotic
   YAML): open read-only for canvas edits, full YAML view still editable.
6. **Scope creep toward a workflow builder:** no new node types, expressions or fields. The graph stays an
   explicit data-dependency DAG.
7. **Design mismatch** (section 1): implementing the artboard literally would add fields the engine rejects.

Decisions needed from the product owner (recommendation first):

1. **Edit strategy:** typed ops on the YAML tree with a semantic-version safety net, gated by the spike
   (recommended), versus accepting normalised rewrites (simpler, noisy diffs, loses comments).
2. **Layout:** inline `metadata.layout` (recommended) versus a sidecar file.
3. **Write trust:** main-process grants with a preload change and an ADR (recommended), versus keeping
   renderer-supplied paths and relying on sidecar checks alone (not recommended for writes).
4. **Node forms:** server-generated field descriptors (recommended for the four v0 types) versus
   hand-written forms with contract tests.
5. **YAML editor:** plain textarea first (recommended), CodeMirror 6 later if needed (runtime dependency of
   the renderer, to be justified per the dependency policy), or CodeMirror from the start.
6. **Artboard divergences** (`foreach`, `depends_on`, timeout, permissions, `vN`): send back to design and
   build only real v0 fields (recommended), or plan the missing engine features first (each its own spec).
7. **Slice order:** F2.4 right after the canvas (recommended) versus the roadmap's strict F2.1 to F2.4 order.
8. **"Point to the value passing through an edge"** (F2.3 criterion): static in the editor (declared
   types, the source output and target input, resolved-input preview where the graph determines it;
   recommended) with a run-value overlay later in the Run Inspector, or require run data now.
9. **Recent graphs:** keep a list of recently opened graphs in the main process (user data) as the only
   "library" until Projects exist (recommended), or none.

## 9. Immediate next steps

1. Answer the decisions in section 8 (at least 1-3, which gate the ADRs).
2. E0: run the fidelity spike on the three repo graphs and attach the measured results to ADR-A.
3. Write ADR-A, B, C and the F2.1-F2.4 specs; review them before any code (AGENTS.md steps 2 and 3).
4. Implement E1 (Go API) before any renderer work, then E2-E5 as separate PRs.
