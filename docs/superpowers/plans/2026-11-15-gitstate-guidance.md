# Gitstate-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/gitstate`.

**Architecture:** Add one guide to `internal/gitstate/` that supplements root
policy and points to Codex, runtime and Gate consumers. Do not change
product code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-15-gitstate-guidance-design.md`.

## Global Constraints

- Do not change product behavior, Git observation contracts, event schema,
  dependencies or CI.
- Preserve distinct ignored-file behavior for adapter metadata and F1.8
  scope checking.
- Do not treat changed paths as authorship or isolation evidence.

## Review Focus

- Controlled Git subprocess and path validation boundaries.
- Index/worktree snapshot semantics and both ignored-file modes.
- Runtime baseline ownership, Gate verdicts and concurrent writer limits.

---

### Task 1: Document shared Git observation

**Files:** Create `internal/gitstate/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/gitstate/` alongside root
policy.

- [ ] Map current Gitstate code, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused Gitstate tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
