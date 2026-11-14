# ADR-0020 - Graph edit operations on the YAML tree

**Status:** proposed
**Date:** 2026-11-14

## Context

The graph editor (F2.1-F2.4, plan `docs/superpowers/plans/2026-11-14-graph-editor.md`) must change
graph files that live in Git and are also written by hand. `graph.Marshal` normalises the whole file:
nodes come out alphabetical (Go map order), comments and blank lines are lost, flow style is rewritten.
A "parse, edit, marshal" save would rewrite `.agents/graphs/demo-vertical.yaml` on every click.

AGENTS.md rule 2 makes the YAML file the execution contract and the UI only an editor of it, so an edit
must change what it means to change and leave the rest alone. The E0 spike
(`docs/validation/E0-yaml-fidelity.md`, tool in `experiments/yaml-fidelity/`) measured eight operations
on the three example graphs and on hostile, CRLF, BOM, long-line and comment fixtures.

Measured: editing the `yaml.Node` tree keeps comments and key order in every case; with a blank-line
restoring pass and an envelope pass (BOM, line endings) a no-op save of the example graphs is
byte-identical; edits change only the lines they must; an edit through a YAML anchor changed a second node
and the semantic-version check caught it; a trailing comment on an anchored key moved to another line.

## Decision

1. **Edits are typed operations applied to the `yaml.Node` tree** of the existing file, by a new package
   `internal/graph/edit`. The renderer never edits YAML text on behalf of the canvas or the forms, and never
   implements graph rules (ADR-0021 puts the operations behind the local API).
2. **A closed, versioned vocabulary.** Operations are JSON objects with an `op` field and an `ops_version`
   (1). The vocabulary covers what the editor does: add/remove/rename node, set or unset a config path,
   add/remove/update an input or output port, add/remove an edge, add/remove a graph input and its binding,
   set or unset a graph output, set or clear the `loop` block and its embedded gate, set `units`, set the
   layout (ADR-0022), set `name`. Exact schemas belong to the F2.1-F2.3 specs; adding an operation needs no
   ADR, changing the meaning of one does. Unknown operations and unknown fields are rejected.
3. **A batch is atomic.** Operations in one request apply in order to one working copy; if any fails the
   whole batch fails and nothing changes. Failures are structured and actionable (AGENTS.md error rules):
   `code`, `message`, the index of the failing operation, the node, edge or path involved, and a suggestion.
4. **Every operation has two implementations that must agree:** one on the tree and one on the typed
   `graph.Definition`. A table test applies both to each corpus file and requires equal semantic versions.
5. **Safety net at runtime.** After applying a batch the server re-parses the result with `graph.Parse`,
   computes `graph.SemanticVersion`, and compares it with the version predicted by applying the same batch
   to the typed definition. For operations classed as presentation (layout, `name`, metadata) the version
   must equal the version before the batch. If the result does not parse or the versions differ, the batch
   is refused with an error and nothing is returned for saving. This catches what the tree edit cannot see,
   such as an edit that reaches a second node through a YAML anchor.
6. **Guarantees** (what the editor promises, as measured by the spike):
   - comments, key order, quoting style and flow style of untouched elements are preserved;
   - a blank line above a key or sequence item (above the comment block directly over it) is preserved,
     keyed by the element's path; new or renamed elements get none;
   - a UTF-8 byte order mark and the file's line-ending convention (LF or CRLF) are preserved;
   - a save with no semantic change writes the identical bytes;
   - comments attached to a deleted element are deleted with it;
   - indentation is normalised to two spaces.
7. **Normalisations that are accepted** (semantically neutral): a document start marker `---` is dropped; the
   spacing before a trailing comment becomes one space; runs of blank lines collapse to one; a folded block
   scalar (`>`) is re-flowed; a leading merge key is written `!!merge <<`.
8. **Refusals.** A file containing an anchor, alias or merge key is opened with a **read-only canvas** and an
   editable YAML view, with a message that says why. The engine does not try to patch through them. A file
   that `graph.Parse` rejects can still be edited as text; canvas edits are offered only for files the engine
   can read.
9. **Raw text edits are saved as typed.** Text typed in the YAML view is validated (parse and graph and
   node-config validation) but never reformatted. Reformatting the whole file is an explicit, separate
   action, and it uses the same tree encoder, not `graph.Marshal`.
10. **A fidelity suite is part of the feature.** The spike's fixtures become golden tests: no-op save is
    byte-identical, each operation's diff is exactly the expected lines, comment **positions** (not only
    presence) are compared, merge-key and anchor files are refused. The guarantees depend on the behaviour of
    `go.yaml.in/yaml/v3` (v3.0.5 measured); upgrading it re-runs the suite.

## Alternatives considered

### A. Parse to `Definition`, mutate, `graph.Marshal`

Pros:

- Smallest implementation; one code path for read and write.

Cons:

- Rewrites the file: loses comments and blank lines, reorders nodes alphabetically, rewrites flow style.
  Every UI save would churn Git diffs and destroy author comments. Rejected.

### B. Text-first: the canvas generates text edits in an editor buffer

Pros:

- Untouched text is exactly preserved.

Cons:

- Generating correct text insertions for nested YAML is fragile and has no structural guarantee; the semantic
  check would still be needed. The YAML view remains available as a view, not as the engine of canvas edits.
  Rejected as the primary path.

### C. A lossless YAML parser (concrete syntax tree) instead of `yaml.Node`

Pros:

- Would preserve blank lines, comment placement and spacing exactly, with no restoring passes.

Cons:

- A new dependency or a new parser to own, review and secure. The spike shows `yaml.Node` plus two small
  passes meets the promise for the repository's graphs. Revisit if the fidelity suite finds a guarantee that
  cannot be met.

## Consequences

### Positive

- Saving from the UI produces minimal, reviewable diffs and keeps comments.
- The editor cannot disagree with the engine: Go owns graph semantics and checks every result.
- The same operations back the canvas, the inspector forms and the edge editor.

### Negative / trade-offs

- A new package with a large test surface and a documented list of normalisations.
- Files with anchors, aliases or merge keys cannot be edited on the canvas.
- Blank-line restoration is path-based and has limits (a blank line between a comment block and its key is
  treated as part of the key's block); the suite must pin the behaviour.
- The guarantees are tied to one YAML library version.

## Compatibility and migration

No schema change: `schema_version: 0` is untouched and so are graph semantics, events and the semantic
version. The operation vocabulary is a new durable API (`ops_version: 1`), versioned so a later vocabulary can
coexist. Existing graph files need no migration. `graph.Marshal` stays as is for the CLI and tests; the editor
does not use it.

## Validation

- A no-op save of every corpus file is byte-identical (0 diff lines), including CRLF and BOM fixtures.
- Each operation has table tests for success and every documented failure; tree and typed implementations
  produce equal semantic versions; presentation operations never change the version.
- Anchor, alias and merge-key files are refused for canvas edits, and a test shows the safety net refusing a
  batch that changes a second node through an anchor.
- Comment positions are unchanged after edits that do not touch them, on all fixtures.
- The API's results equal `agentplane graph validate` for the same text (ADR-0021).
