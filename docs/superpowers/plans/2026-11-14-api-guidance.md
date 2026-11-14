# API-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance for API changes.

**Architecture:** Add one guide in `internal/api/`; keep the root policy and
existing source ownership. Document the Electron and CLI boundaries where
they affect API behavior. Do not change product code.

**Tech Stack:** Markdown and the existing Go/desktop test suites; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-api-guidance-design.md`.

## Global Constraints

- Preserve root `AGENTS.md` as the repository-wide policy.
- Do not change product behavior, public contracts, dependencies or CI.
- Distinguish API authentication from Electron's renderer allowlist.
- Do not describe path-based read/run compatibility as grant-only.

## Review Focus

- In production `serve`, the store append precedes the hub sink; SSE replay
  has no gap or duplicate when joining a live run.
- Decision repeats are scoped to the original run and return its first result.
- Document edits are in memory; saves use the on-disk base hash and a grant.
- Grant registration is blocked from renderer forwarding, not by a separate
  Electron-main identity check in the API.

---

### Task 1: Document the API boundaries

**Files:** Create `internal/api/AGENTS.md` and this design/plan pair.

**Interface:** The local guide applies to `internal/api/`; it points to
cross-component ownership without changing it.

- [ ] Map current API source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run `go test ./internal/api/...` and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
