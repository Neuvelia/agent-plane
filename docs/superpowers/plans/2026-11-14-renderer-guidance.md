# Renderer-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance for React renderer changes.

**Architecture:** Add one guide to `desktop/src/` and link the sibling
Electron boundary without changing product code. Keep detailed editor
guidance as a later small step.

**Tech Stack:** Markdown and existing Vitest/Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-renderer-guidance-design.md`.

## Global Constraints

- Do not change product behavior, public contracts, dependencies or CI.
- Keep event-derived run state separate from API lists and local YAML drafts.
- Keep graph semantics and operation decisions in the Go sidecar.
- Describe only F2.1 slices currently present on `main`.

## Review Focus

- Replayed event sequences do not double-apply; a dropped stream is not a
  fabricated interrupted run.
- List failures are errors with Retry, not empty or current stale lists.
- Editor draft/history is legitimate local state; canvas decisions come from
  the sidecar and use its returned text.
- Story states use recorded event/API fixtures; PL/EN and design checks keep
  presentation changes testable.

---

### Task 1: Document the renderer boundaries

**Files:** Create `desktop/src/AGENTS.md` and this design/plan pair.

**Interface:** The local guide applies to `desktop/src/`; it points to
cross-component owners without changing them.

- [ ] Map current source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused desktop tests, `npm run check` and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
