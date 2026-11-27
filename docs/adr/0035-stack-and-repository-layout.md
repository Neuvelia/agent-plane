# ADR-0035 - Runtime stack and repository layout

**Status:** accepted (owner, 2026-11-26)
**Date:** 2026-11-26
**Source:** roadmap audit, decisions D9 and D10

## Context

The architecture plan leaves Go vs Rust open and recommends Tauri over
Electron. The roadmap §10 proposes a `packages/` layout (`graph-schema`,
`execution-core`, `event-model`, `adapters`, `workspace-manager`,
`local-store`, `runner-daemon`, `cli`, `ui`, `cloud-api`). The repo is Go with
an Electron desktop (ADR-0019, ADR-0030 rejected Tauri) and a
`cmd/` + `internal/` layout (AGENTS.md, `docs/codebase-map.md`).

## Decision

1. Go for the runtime and CLI; Electron + React for the desktop. Tauri is not
   used (ADR-0030).
2. The `cmd/` + `internal/` layout is canonical. The roadmap §10 package names
   are kept only as **ownership areas** (`owner_package` in
   `docs/roadmap/features.yaml`), mapped to directories in
   `docs/roadmap/canonical-spec.md`.

## Alternatives considered

### Restructure into `packages/`

Cons: a large move with no behavior change; conflicts with the
clean-architecture layering already enforced by `internal/archtest`.

## Consequences

Registry entries name roadmap areas; readers use the canonical-spec table to
find code.

## Compatibility and migration

None.

## Validation

The mapping table in the canonical spec is checked by review when packages move.
