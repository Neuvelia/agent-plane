# H9: Store-local agent guidance

## Intent

An agent changing local run history should find the JSONL owner, durable
guarantees, corruption behavior and cross-package tests. The root and
`internal/events/AGENTS.md` guidance remain in force.

## Approach

Add `internal/store/AGENTS.md` alongside the store. Ground it in the F2.9
slice and ADR-0019, current `store.go` behavior, and CLI/serve call sites.
Describe the limited fsync guarantee, the exact graph snapshot, reader
validation and the split between store mechanics and caller error policy.

This is contributor guidance only. Product code, JSONL layout, event schema,
dependencies and CI rules do not change.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide distinguishes process-kill durability from power-loss behavior.
- It describes retained event order, sequence gaps, truncation, corruption,
  exclusive creation and owner-only permissions accurately.
- It places graph-secret screening in CLI preflight, event redaction before
  storage, and CLI/serve persistence-failure policy with their owners.
- It points to store unit tests and integration tests for CLI and served runs.
- Focused store tests and `make check` pass.

## Risks

The architecture document describes a future SQLite-based store; H9 must
name the currently implemented JSONL slice. A local history directory may
contain sensitive graph bytes, so the guide must not imply that the store
redacts its graph snapshot.
