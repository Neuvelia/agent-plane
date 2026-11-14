# Editor-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance for the graph editor.

**Architecture:** Add one guide to `desktop/src/editor/` that supplements
the renderer guide and points to Electron, API and graph-edit owners. Do not
change product code.

**Tech Stack:** Markdown and existing Vitest/Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-editor-guidance-design.md`.

## Global Constraints

- Do not change product behavior, public contracts, dependencies or CI.
- Keep the draft edit hash separate from the saved file's base hash.
- Keep graph validation, operation semantics and file writes in Go.
- Describe only F2.1 E1/E2.1-E2.3 as present on `main`.

## Review Focus

- Typed text and operation results share one bounded undo/redo history;
  stale answers do not replace newer draft text.
- File changes produce conflict handling, and a refused save blocks a run.
- The canvas consumes engine `can_connect` results and writes layout through
  operations; it does not implement graph rules.
- Parse errors and anchors leave the YAML view editable but canvas read-only.

---

### Task 1: Document the editor boundaries

**Files:** Create `desktop/src/editor/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `desktop/src/editor/` alongside
the parent renderer policy.

- [ ] Map current editor source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused desktop tests, `npm run check` and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
