# Local Task Journal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make long coding-agent tasks resumable through a local, Git-ignored journal in each worktree.

**Architecture:** Preserve the existing tracked `TASKS.md` as a reference document, then reserve the root name for a Git-ignored worktree journal. Add concise instructions to the existing root agent guide and development workflow. No executable code or new dependency is needed.

**Tech Stack:** Git ignore rules and Markdown.

**Spec:** `docs/superpowers/specs/2026-11-14-task-journal-design.md`.

## Global Constraints

- The journal is local working state, not a committed feature spec or report.
- Do not record secrets, raw model output, or sensitive repository content.
- Do not change product runtime, graph, API, event, or CI contracts.

## Review Focus

- A root journal must be ignored in every worktree; a nested `TASKS.md` must not be hidden by this rule.
- The existing implementation queue must remain available with its contents intact.
- A resumed task must have a concrete next action and last check result, not only a vague status.
- The new instructions must not make a journal mandatory for one-step work.

---

### Task 1: Add local task continuity

**Files:** Move `TASKS.md` to `docs/reference/implementation-queue.md`; modify `.gitignore`, `AGENTS.md`, `docs/development-workflow.md`, `README.md`, `docs/reference/README.md`.

**Interfaces:** Root `TASKS.md` is a local Markdown checklist in the current worktree.

- [ ] Verify that root `TASKS.md` is tracked and preserve its contents by moving it to `docs/reference/implementation-queue.md`.
- [ ] Add `/TASKS.md` to `.gitignore`, update the queue references and document journal fields, update points, and the resume step.
- [ ] Verify `git check-ignore -v TASKS.md` matches the repository rule and `git check-ignore -q internal/TASKS.md` does not match it.
- [ ] Create a local `TASKS.md`, confirm it is absent from `git status`, and remove no user data.
- [ ] Run `make check`, inspect the complete diff and commit only the planned files.
