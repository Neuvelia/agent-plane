# H3: Graph-local agent guidance

## Intent

An agent working on graph definitions or editor operations should find the
owner of a change and preserve the durable YAML, validation, binding and edit
contracts. The root `AGENTS.md` remains the repository-wide policy.

## Approach

Add `internal/graph/AGENTS.md` for schema, parsing, validation, bindings,
values and semantic versioning. Add a narrower `internal/graph/edit/AGENTS.md`
for tree-based YAML operations and fidelity. Update `docs/codebase-map.md` to
show the existing `edit` package and its direct import boundary.

One large root guide would burden unrelated modules. One graph guide covering
both packages would blur the important distinction between `graph.Marshal`
and editor-preserving edits. Nested guides keep each rule next to its code.

## Scope and acceptance

- Both guides inherit the root policy and describe current code only. They do
  not treat future F2.2/F2.3 operations as implemented.
- Every named source, test, spec and ADR path exists on the target branch.
- The graph guide keeps structural parsing distinct from semantic validation
  and executor configuration checks, and preserves deterministic diagnostics,
  explicit data bindings, typed values and semantic version behavior.
- The edit guide keeps atomic typed/tree agreement, read-only refusal for
  anchors/aliases/merge keys, YAML fidelity and the API-only save boundary.
- The codebase map matches direct production imports from `go list`.
- An independent reviewer can use the guides to locate a binding diagnostic
  change and a no-op YAML fidelity change, with the matching tests.
- `go test ./internal/graph/...` and `make check` pass. No product code,
  public contract, dependency or CI rule changes.

## Risks

Guidance can become stale as editor slices land. Keep references to implemented
operations and update the guide when owners or contracts change. Do not imply
that graph editing is file writing or that `graph.Validate` checks every
executor's configuration.
