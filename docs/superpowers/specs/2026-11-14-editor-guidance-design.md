# H7: Editor-local agent guidance

## Intent

An agent changing the graph editor should find the draft, API, canvas and
save owners and preserve their data boundaries. The root and renderer
`AGENTS.md` files remain in force.

## Approach

Add `desktop/src/editor/AGENTS.md` beside the editor. Link current F2.1,
ADR-0020/0021/0022, the Electron and Go guides, and focused tests. Explain
the two different meanings of `base_hash`, stale validation, undo/redo,
engine-owned canvas operations, conflict handling and Save and run.

This changes contributor guidance only. No product code, public API, event
schema, dependency or CI rule changes.

## Scope and acceptance

- Every named source, test, spec and ADR path exists on the target branch.
- The guide distinguishes draft-text edit hashes from on-disk save hashes,
  and identifies when a refused operation or save leaves draft/file unchanged.
- It preserves engine authority for graph validity, `can_connect`, YAML
  fidelity and file writes; local layout helpers only choose presentation.
- It describes current E1/E2.1-E2.3 code without claiming F2.2-F2.4.
- An independent reviewer can locate a stale-validation or save-conflict
  change and a layout-version change with tests, and identify the missing
  focused refusal-UI test for rejected connections.
- Focused desktop tests, `npm run check` and `make check` pass.

## Risks

F2.1 is marked implemented while its acceptance checklist remains unchecked;
the guide must follow code and tested behavior, not claim blanket acceptance.
The editor has local draft state, but the Go sidecar remains the authority
for graph semantics and saved-file writes.
