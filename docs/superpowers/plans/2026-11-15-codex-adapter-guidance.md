# Installed Codex Adapter Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/agent/codex`.

**Architecture:** Add one guide inside the Codex adapter that supplements
root and parent Agent guidance and points to Git state, runtime and CLI
owners. Do not change product code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-15-codex-adapter-guidance-design.md`.

## Global Constraints

- Do not change product behavior, CLI protocol, graph/event schemas,
  dependencies or CI.
- Keep raw provider output private, environment explicit and process
  cancellation bounded.
- Keep adapter changed-file evidence distinct from runtime scope checking.

## Review Focus

- Detection/argv/environment and process-group shutdown.
- Strict final result and safe normalized progress boundaries.
- Invocation-relative Git changes, ignored-path handling and F1.8 limits.

---

### Task 1: Document the installed Codex adapter

**Files:** Create `internal/agent/codex/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/agent/codex/` alongside
root and `internal/agent/AGENTS.md`.

- [ ] Map current Codex source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused Codex tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
