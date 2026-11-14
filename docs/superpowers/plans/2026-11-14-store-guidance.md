# Store-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/store`.

**Architecture:** Add one guide to `internal/store/` that supplements root
policy and points to event, CLI and API owners. Do not change product code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-store-guidance-design.md`.

## Global Constraints

- Do not change product behavior, JSONL layout, event schema, dependencies
  or CI.
- Describe the current F2.9 JSONL slice, not later SQLite or resume work.
- Keep caller policy and secret screening outside `internal/store`.

## Review Focus

- The sync guarantee covers acknowledged non-progress facts after process
  kill, not directory/graph survival after power loss.
- Reader/list behavior distinguishes interrupted, truncated and corrupt.
- Store writes retained facts; event redaction and sequence assignment happen
  before `Writer.Append`.

---

### Task 1: Document the store boundaries

**Files:** Create `internal/store/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/store/` alongside root
policy.

- [ ] Map current store source, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused store tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
