# Graph edit package guidance

The root `AGENTS.md` and parent `internal/graph/AGENTS.md` also apply. This
package edits a graph document in memory; it does not save a file.

## Find the owner

| Concern | Start here |
| --- | --- |
| Versioned operation vocabulary and strict JSON decode | `ops.go` |
| Atomic batch and typed/tree safety net | `apply.go` |
| Node, edge and layout operations | `nodes.go`, `edges.go`, `layout.go` |
| Inspector operations: config, ports and their references, loop (whole and targeted), units (F2.2) | `config.go`, `ports.go`, `refs.go`, `loop.go`, `values.go` |
| YAML tree helpers and read-only constructs | `tree.go` |
| Config value text for the inspector's Advanced section (read-only) | `snippets.go` |
| Blank lines, BOM, line endings and encoding | `envelope.go` |
| Structured operation failures | `errors.go` |

## Preserve these contracts

- `ops.go` defines the current version-1 operation vocabulary (F2.1 and F2.2).
  A JSON value enters through `values.go`, which keeps object key order and
  writes numbers so YAML reads them as the same type; nested config mappings
  are `graph.NodeConfig` in the typed prediction, as `graph.Parse` reads them.
  Port renames and removals go through `refs.go`, the one list of places a
  port is referenced. `Decode`
  strictly parses the operation array; the API checks `ops_version` before
  calling it. A batch in `Apply` is bounded and atomic: on an operation or
  safety-net error, return no edited text. Add operations only in their own
  activated feature scope.
- Apply each edit to the `yaml.Node` tree and an independent typed
  `graph.Definition` prediction. Re-parse the encoded text and compare the
  full typed result; presentation-only edits must keep `SemanticVersion`.
  Do not replace this path with `graph.Marshal`, which rewrites untouched YAML.
- Preserve the fidelity rules in `doc.go`: untouched comments, key order,
  quoting and flow style; path-associated blank lines; BOM and LF/CRLF; and
  byte-identical no-op files covered by fixtures. Accepted normalizations are
  listed there; do not promise stronger byte fidelity than those tests prove.
- `Inspect` reports anchors, aliases and merge keys as read-only reasons;
  `Apply` refuses canvas operations on them. `graph.Parse` may still read such
  YAML; this refusal does not prohibit raw text handling by the caller. A
  parseable graph may still have semantic diagnostics; the caller runs
  `graph.Validate`.
- `can_connect` must agree with `graph.Validate` on references, types,
  providers and cycles. `Apply` returns text and operation results but never
  writes a file; granted, conflict-checked atomic saving belongs to
  `internal/api/docs.go`, not this package.

Read `docs/features/F2.1-graph-canvas.md`, `docs/adr/0020-graph-edit-operations.md`,
`docs/adr/0021-local-graph-document-api-and-write-trust.md`,
`docs/adr/0022-graph-layout-metadata.md`, and
`docs/validation/E0-yaml-fidelity.md` before changing edit semantics.

Use `edit_test.go` and `testdata/` for operation, refusal, safety-net and
fidelity regressions. Run `go test ./internal/graph/...`; if the API contract
changes, also run `go test ./internal/api/...` with
`internal/api/docs_test.go`. Update this guide when the implemented operation
vocabulary or fidelity promise changes.
