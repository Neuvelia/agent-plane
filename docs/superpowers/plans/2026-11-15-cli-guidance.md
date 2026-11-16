# CLI-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `cmd/agentplane`.

**Architecture:** Add one guide to the CLI package that supplements root
policy and points to graph, runtime, event, store and API owners. Do not
change product code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-15-cli-guidance-design.md`.

## Global Constraints

- Do not change product behavior, CLI JSON/exit contracts, event schema,
  dependencies or CI.
- Describe bounded live delivery per ADR-0011, not historical ADR-0007
  backpressure behavior.
- Keep validation, state, storage mechanics and API security with their
  current owners.

## Review Focus

- CLI/API validation parity, run preflight and deterministic diagnostics.
- JSON schema 1, exit-code meanings and watch prefix/tail replay.
- Secret handling, opt-in CLI history and store-before-client server order.

---

### Task 1: Document CLI and sidecar composition

**Files:** Create `cmd/agentplane/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `cmd/agentplane/` alongside root
policy.

- [ ] Map current command source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused CLI tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
