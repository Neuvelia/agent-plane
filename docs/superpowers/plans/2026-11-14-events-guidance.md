# Events-local Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give coding agents accurate local navigation and contract guidance
for `internal/events`.

**Architecture:** Add one guide to `internal/events/` that supplements root
policy and points to the producer and consumer owners. Do not change product
code.

**Tech Stack:** Markdown and existing Go checks; no new dependency.

**Spec:** `docs/superpowers/specs/2026-11-14-events-guidance-design.md`.

## Global Constraints

- Do not change product behavior, wire contracts, dependencies or CI.
- Preserve historical decoding, typed payload validation, bounded progress
  and collector sequence/order semantics.
- Distinguish producer redaction from event collection.

## Review Focus

- Schema 9 is emitted; JSON readers accept schemas 1-9 without inventing
  metadata for old facts.
- Sequence gaps, omission marker and live prefix reflect the collector.
- Persistence is optional and ordered; storage and state transitions belong
  to other packages.

---

### Task 1: Document the event boundaries

**Files:** Create `internal/events/AGENTS.md` and this design/plan pair.

**Interface:** The child guide applies in `internal/events/` alongside root
policy.

- [ ] Map current event source files, contracts and targeted tests.
- [ ] Check every referenced path against the current checkout.
- [ ] Run focused event tests and `make check`.
- [ ] Inspect the complete diff and obtain independent read-only review.
- [ ] Commit, push and create a PR without merging it.
