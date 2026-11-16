# Desktop CI Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the existing desktop package check on every push and pull
request as an independent CI job.

**Architecture:** Keep the Go job unchanged and add a Node job in the existing
workflow. Document the resulting check coverage in existing developer guides.

**Tech Stack:** GitHub Actions, Node/npm, existing Go and desktop checks.

**Spec:** `docs/superpowers/specs/2026-11-16-desktop-ci-check-design.md`.

## Global Constraints

- Keep `push` and `pull_request` triggers and the existing Go job.
- No secrets, `pull_request_target`, browser install, real Electron run,
  product code or new dependency.
- Desktop checkout must not persist credentials; permissions stay read-only.

## Review Focus

- GitHub workflow syntax and independent job status.
- Nested npm lockfile cache and clean install on Node 24/Linux.
- Install-time lifecycle scripts and absence of publishing credentials.
- Honest description of `npm run check` versus browser/e2e coverage.

---

### Task 1: Add and verify the desktop CI gate

**Files:** Edit `.github/workflows/ci.yml`, `desktop/AGENTS.md`,
`docs/development-workflow.md`; create this design/plan pair.

**Interface:** A CI job named `desktop` reports the result of the package's
existing `npm run check` on pushes and pull requests.

- [ ] Confirm the local locked install and `npm run check` work with
  `npm ci --ignore-scripts`.
- [ ] Add the independent job, preserving the Go job and triggers.
- [ ] Update guide text to name the new CI coverage and limits.
- [ ] Parse/review the workflow and run `npm run check` plus `make check`.
- [ ] Inspect the full diff and obtain independent read-only review.
- [ ] Commit, push, open a PR and inspect both hosted CI job results.
