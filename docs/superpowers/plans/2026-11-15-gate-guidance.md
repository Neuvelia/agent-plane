# Gate-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/gate`.

**Architecture:** Add one guide to `internal/gate/` that supplements root
policy and points to runtime, events and Gitstate owners. Do not change
product code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-15-gate-guidance-design.md`.

## Global Constraints

- Do not change product behavior, graph/event schemas, CheckResult shape,
  dependencies or CI.
- Preserve valid red versus operational-error distinction and the fixed
  read-only command restriction.
- Keep reported-file data distinct from Git-observed scope evidence.

## Review Focus

- Config/port validation and exact predicate semantics.
- Fixed diff process, path preflight and bounded capture.
- CheckResult observation, Git warn/deny and loop feedback boundaries.

---

### Task 1: Document deterministic Gate boundaries

**Files:** Create `internal/gate/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/gate/` alongside root
policy.

- [ ] Map current Gate source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused Gate tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
