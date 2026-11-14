# Graph-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate, local navigation and contract guidance for graph and YAML-edit changes.

**Architecture:** Use a parent guide for graph semantics and a child guide for fidelity-preserving editor operations. Correct the codebase map to include the already implemented edit package and its import into the API; do not change execution code.

**Tech Stack:** Markdown and the existing Go test suite; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-graph-guidance-design.md`.

## Global Constraints

- Preserve root `AGENTS.md` as the repository-wide policy.
- Do not change product behavior, public contracts, dependencies or CI.
- Distinguish structural parse, semantic validation and executor config checks.
- Describe only editor operations implemented in `internal/graph/edit/ops.go`.

## Review Focus

- `graph.Parse` may parse anchors; the editor refuses canvas operations on them.
- `graph.Marshal` normalizes YAML and must not replace tree edits for the editor.
- A parseable graph may still have semantic diagnostics; `edit.Apply` is not a full validator.
- An absent optional graph input may leave an optional target unset; a missing bound node output is an error even for an optional target.
- Presentation edits must not change `graph.SemanticVersion`.
- The edit package returns text/results without writing; the API owns granted file saves.

---

### Task 1: Document graph and editor boundaries

**Files:** Create `internal/graph/AGENTS.md` and `internal/graph/edit/AGENTS.md`; modify `docs/codebase-map.md`.

**Interface:** The parent guide applies to `internal/graph/` and its subpackages; the child guide adds rules within `internal/graph/edit/`.

- [ ] Map current graph/edit source files and targeted tests in their respective guides.
- [ ] State the verified parser, validator, binding, hash, operation, fidelity and save-boundary rules.
- [ ] Update the codebase map's package/import table using production `go list` output.
- [ ] Check every referenced path; run `go test ./internal/graph/...` and `make check`.
- [ ] Inspect the complete diff and commit only the planned files for independent review.
