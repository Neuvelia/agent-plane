# Electron-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance for Electron boundary changes.

**Architecture:** Add one guide to `desktop/electron/`; point to the Go API
and renderer types without changing their code. Preserve the root policy.

**Tech Stack:** Markdown and existing Go, Vitest and Electron checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-electron-guidance-design.md`.

## Global Constraints

- Do not change product behavior, public contracts, dependencies or CI.
- Distinguish the generic request allowlist from SSE subscription IPC.
- Distinguish grant-scoped editor writes from legacy path-based run reads.
- Describe only F2.1 slices currently present on `main`.

## Review Focus

- Main alone holds the sidecar address and token; preload exposes narrow calls.
- Generic renderer requests cannot register/revoke grants.
- Main registers file paths from dialogs or its own recent list; the editor
  renderer sees grants and labels, while legacy New run can still see a path.
- The sidecar validates grants and performs saves; Electron does neither.

---

### Task 1: Document the Electron boundary

**Files:** Create `desktop/electron/AGENTS.md` and this design/plan pair.

**Interface:** The local guide applies to `desktop/electron/`; it points to
cross-component ownership without changing it.

- [ ] Map current source files and focused tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused desktop tests, `npm run check` and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
