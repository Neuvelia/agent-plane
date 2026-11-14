# Runtime-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents a short, accurate guide at the runtime package boundary.

**Architecture:** Add `internal/runtime/AGENTS.md` beneath the inherited root guide. It maps current files and tests to runtime responsibilities and states the package-specific invariants without changing execution code.

**Tech Stack:** Markdown and the existing Go test suite; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-runtime-guidance-design.md`.

## Global Constraints

- Preserve root `AGENTS.md` as the repository-wide policy.
- Do not change product behavior, public contracts, dependencies, or CI.
- Describe Git-observed scope checking as detection, not write isolation.

## Review Focus

- Do not claim every event is coordinator-emitted: human approval facts and bounded worker observations are exceptions.
- Do not claim every successful node needs a Gate: non-loop Shell nodes retain existing success semantics.
- Do not imply accepted unit values make shared workspace files immutable.
- Use current test and ADR paths; avoid stale or invented names.

---

### Task 1: Add the runtime-local guide

**Files:** Create `internal/runtime/AGENTS.md`.

**Interface:** Local instructions apply to source and tests under `internal/runtime/` and inherit the root `AGENTS.md`.

- [ ] Write a file map for preflight, scheduling, batches, acceptance, loops/units/Gates, human decisions, workspaces and observations.
- [ ] State runtime-specific invariants for state ownership, ordered acceptance, red/green retry, event facts, cancellation and import direction.
- [ ] Point to the existing targeted test groups and the conditions for a runtime race test.
- [ ] Check every referenced path, run `go test ./internal/runtime/...` and `make check`, and inspect the complete diff.
- [ ] Commit the documentation-only change for independent review.
