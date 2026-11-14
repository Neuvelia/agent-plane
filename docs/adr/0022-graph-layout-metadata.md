# ADR-0022 - Canvas layout stored in `metadata.layout`

**Status:** accepted
**Date:** 2026-11-14

## Context

The canvas needs node positions. Domain-model invariant 7 says visual canvas metadata must not change
execution semantics; AGENTS.md says semantic content stays separate from visual layout metadata. The graph
spec already reserves `metadata` as an open mapping for "future canvas layout", and `graph.SemanticVersion`
removes `name` and `metadata` before hashing (an existing test shows two definitions that differ only in
metadata have the same version).

The desktop map today uses a deterministic dagre layout and stores nothing.

## Decision

1. **Positions live inline in the graph file**, under `metadata.layout`:

   ```yaml
   metadata:
     layout:
       nodes:
         plan: {x: 120, y: 80}
         backend: {x: 420, y: 40}
   ```

2. **Shape.** `layout.nodes` maps a node id to a flow mapping with integer `x` and `y` in canvas pixels,
   within -100000..100000. No other keys are defined; unknown keys under `layout` are preserved and ignored.
3. **Reading is tolerant.** A malformed layout, an entry for an unknown node, or a non-integer coordinate is
   ignored for that node and never produces an engine diagnostic: `metadata` is not validated by the engine.
   The editor may show a quiet notice.
4. **Fallback.** A node without an entry gets the deterministic dagre position. Nothing is written until the
   user moves a node (or runs an explicit "Arrange" action, which writes all positions).
5. **Writing** is the `set_layout` operation of ADR-0020. It is a presentation operation: the semantic
   version must be unchanged (the safety net enforces it). Coordinates are rounded to integers; new entries
   are added in node-id order; the `metadata` block is inserted before `inputs` when absent (as measured in
   the spike), so a first move adds four lines.
6. **Deleting or renaming a node** removes or renames its layout entry in the same batch.
7. **Not affected:** the semantic version, run identity and run events. A moved node never changes
   `graph_version` in events.
8. The spec text (`docs/graph-spec.md`, Metadata section) documents the shape in the slice that implements it.

## Alternatives considered

### A. Sidecar file (`graph.layout.json` next to the graph)

Pros:

- Moving a node never touches the graph file, so Git diffs of the graph stay clean.

Cons:

- Two files must travel together; copying, renaming or committing only one silently loses layout. A second
  write path and a second trust surface (ADR-0021 would need a second grant). It contradicts the spec's
  wording "layout is metadata". Rejected.

### B. Store layout only in the desktop's user data keyed by graph path or id

Pros:

- No file changes at all.

Cons:

- Layout is lost on another machine or clone, and keyed by path breaks on move or rename. The graph would
  not be portable as a self-contained definition. Rejected.

### C. Always auto-layout, store nothing

Pros:

- No diff noise, no format to maintain.

Cons:

- Users cannot arrange a graph they will read repeatedly; F2.1 requires layout to be saved as metadata.
  Auto-layout stays as the fallback and the "Arrange" action, not the only mode.

## Consequences

### Positive

- One file carries the whole definition, in line with the spec.
- Layout is already outside the semantic version, so runs and caching are unaffected.

### Negative / trade-offs

- Moving a node produces diff lines in the graph file. Mitigated by integers, stable key order, a single
  line per node, and writing only on explicit moves.
- Shared graphs edited by several people can conflict on layout lines; resolving them is a normal merge.

## Compatibility and migration

No schema change; `metadata` is already an open mapping. Existing graphs have no layout and render with the
fallback. An older engine ignores `metadata`. Graph files with a `metadata.layout` of a different shape keep
working: the editor ignores what it cannot read and preserves it.

## Validation

- A test moves a node and shows the semantic version is unchanged (in addition to the existing
  metadata-differs test), and that the first move adds exactly the layout block.
- Malformed layouts (wrong types, unknown ids, out-of-range values) are ignored without an engine
  diagnostic and survive a save unchanged.
- Deleting and renaming a node updates the layout entry in the same batch.
- e2e: move a node, save, and the diff of the file is limited to `metadata.layout`; the run's `graph_version`
  is identical before and after.
