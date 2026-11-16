# Shell-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/shell`.

**Architecture:** Add one guide to `internal/shell/` that supplements root
policy and points to runtime, graph and CLI owners. Do not change product code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-16-shell-guidance-design.md`.

## Global Constraints

- Do not change product behavior, graph/event schemas, dependencies or CI.
- Keep raw process evidence separate from redacted runtime log events.
- Do not claim Shell execution provides descendant termination, sandboxing or
  rejected-attempt rollback.

## Review Focus

- Static config and explicit input-to-environment mapping before process start.
- Cancellation races, inherited output pipes and typed failure evidence.
- Runtime log redaction and Shell-loop retry boundaries.

---

### Task 1: Document Shell executor boundaries

**Files:** Create `internal/shell/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/shell/` alongside root
policy.

- [ ] Map current Shell source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused Shell tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
