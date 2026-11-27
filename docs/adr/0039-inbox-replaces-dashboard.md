# ADR-0039 - The Inbox triage queue replaces the Dashboard as the landing screen

**Status:** proposed
**Date:** 2026-11-27
**Amends:** F2.5a requirement 11 (Dashboard) and requirement 4 (Sidebar and global bar)
**Feature:** F2.5b (`docs/features/F2.5b-inbox-redesign.md`)

## Context

F2.5a made a four-card Dashboard (Needs attention, Active runs, Outcomes, Quick actions) the
desktop landing screen. Every row only links away, so deciding an approval or seeing why a run
stopped means leaving the screen. The revised Tarmind Dashboard UX canvas makes the landing screen
a triage queue with the selected item's evidence and action beside it.

## Decision

1. The landing screen is the **Inbox** (PL "Skrzynka"); the Sidebar entry is renamed accordingly.
   F2.5a requirement 11 is replaced by F2.5b's functional requirements.
2. The Inbox is presentation only. It reads the same lists (`GET /runs`, `GET /approvals`) and the
   same per-run event stream and reducer as the run inspector; it decides approvals through the
   existing decision endpoint with the F2.10 rules (applied only on `approval_decided`, `409` is
   already decided, disabled while the stream is lost).
3. The Sidebar follows the canvas: the name without the logo, New run as a button, Recent runs
   (the five newest, from `GET /runs`), a Later group for the disabled surfaces and the profile in its
   footer; the global bar keeps only the connection indicator. On macOS the traffic lights move
   2px up to sit centred in the 44px brand strip.
4. The 7-day Outcomes dot chart gives way to a strip of the newest 14 runs; Quick actions stay as
   two always-visible buttons.

## Alternatives considered

### Keep the Dashboard and add a decision panel to Needs attention

Pros: smaller change; keeps the 7-day chart.

Cons: still four cards competing for attention; the canvas design is not followed.

### Make Approvals the landing screen

Pros: decision in place already exists.

Cons: stopped and running runs would need a second landing place; the queue would still be split.

## Consequences

### Positive

- One place to see and act on what needs a person; fewer screen changes per decision.
- No engine, API, event or trust-boundary change.

### Negative / trade-offs

- The 7-day outcome history moves out of the landing screen (the runs list keeps it complete).
- Each selected item opens one event stream, as the run inspector does.

## Compatibility and migration

No stored data, graph, event or API changes. Test IDs `dashboard`/`nav-dashboard` became
`inbox`/`nav-inbox`; the e2e script and stories follow.

## Validation

`desktop/test/inbox.test.ts`, the Inbox stories in the EN/PL story smoke (axe) and the
real-Electron e2e (`desktop/e2e/demo.cjs`).
