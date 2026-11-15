# Provider-neutral Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/agent`.

**Architecture:** Add one guide at `internal/agent/` that supplements root
policy and points to runtime, events and Codex owners. Do not change product
code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-15-agent-guidance-design.md`.

## Global Constraints

- Do not change product behavior, graph/event schemas, adapter interfaces,
  dependencies or CI.
- Keep the runtime provider-neutral and adapter lookup explicit per caller.
- Do not describe advisory paths as isolation or infer unknown capability and
  usage values as false or zero.

## Review Focus

- Config validation and exactly one task source.
- Correlation, progress interface precedence and correction capability.
- Declared outputs, separate evidence and event-safe error summaries.

---

### Task 1: Document the neutral Agent boundary

**Files:** Create `internal/agent/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/agent/`, including
`internal/agent/codex/`, alongside root policy.

- [ ] Map current Agent source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused Agent tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
