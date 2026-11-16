# Desktop Workspace Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate workspace-level navigation, generated
artifact rules and check selection for `desktop/`.

**Architecture:** Add one package-root guide that delegates implementation
contracts to existing child guides. Do not change product code or commands.

**Tech Stack:** Markdown, npm/Vite/Storybook and existing Go checks; no new
dependency.

**Spec:** `docs/superpowers/specs/2026-11-16-desktop-workspace-guidance-design.md`.

## Global Constraints

- Do not change product behavior, dependency versions, CI or API/event schemas.
- Keep the Electron main/preload/renderer authority and trust boundaries clear.
- Distinguish build-only checks from real-browser and real-Electron checks.

## Review Focus

- Command matrix versus `Makefile` and `desktop/package.json`.
- Generated committed files versus ignored build output.
- Story fixture provenance and no-network/resource boundaries.

---

### Task 1: Document desktop workspace rules

**Files:** Create `desktop/AGENTS.md` and this design/plan pair.

**Interface:** The desktop-root guide applies alongside root policy and is
specialized by guides in `electron/`, `src/` and `src/editor/`.

- [ ] Map current commands, scripts, generated files and ownership boundaries.
- [ ] Check every referenced path against the current checkout.
- [ ] Run `npm run check` in `desktop/` and root `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
