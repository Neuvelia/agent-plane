# ADR-0048 - Welcome and setup as a four-step setup

**Status:** proposed (2026-11-27)
**Date:** 2026-11-27
**Features:** F2.11 (welcome screen), F2.12 (templates)
**Amends:** F2.11 requirements 9, 12 and 14 (presentation of the welcome screen); no API, event or storage change

## Context

The F2.11 welcome screen was one page: two sentences, the capability list and
three actions ("Start from a template", "Open a graph", "Skip"). The design
canvas "Setup, revised" (artboards 1 Welcome, 2 Check this computer, 3 Agent
nodes, 4 First graph, plus "checking" and "engine not connected") replaces it
with a four-step setup told in motion, with Skip always in the header and the
check never blocking progress.

## Decision

1. "Welcome and setup" is a full-window setup (sidebar kept, global bar and
   breadcrumb hidden) with a header (title, four progress segments that each
   jump to their step, "Skip setup"), one step at a time, and a footer (Back, a
   hint, one primary action).
2. Steps: (1) the four node kinds as an animated example graph and three
   statements (checked before it runs, runs on this computer, nothing leaves
   it); (2) the F2.11 capability report as a scan, with a one-line verdict
   decided only from the engine answering and the `shell` row; every report row
   keeps its status word, glyph, meaning and guidance (requirements 9 and 11 are
   unchanged in content); (3) agent nodes run from the CLI, with the exact
   `agentplane run <graph.yaml> --adapter codex --codex-bin <path>` command and
   Copy; (4) a first graph: the suggested template, `dependency-update`,
   `human-approval` and Blank as cards that draw their nodes in file order,
   "Open a graph I already have…". The cards carry the F2.12 requirement-7
   notices from the same report: a platform the commands do not support
   replaces "Runs as is" and removes "Recommended", and each required tool is
   shown with its status ("not checked" before the report arrives).
3. The last step's primary action creates the picked graph directly (the same
   `newGraph` grant and template draft as the F2.12 picker); it no longer opens
   the picker. The picker stays the editor's "New graph" flow. Setup is left
   only once the save dialog led somewhere: a new document opens the editor
   and completes setup; a cancelled dialog stays on setup; unsaved text or an
   error opens the editor, where its prompt or message is shown.
3a. Requirement 12, engine disconnected: instead of no rows, the check step
   shows two explanatory rows that are not report data ("Engine: Not
   connected" with the existing guidance, "Git, agent CLIs: Not checked") and
   Retry. No stale report row is ever shown. While the engine is unreachable
   the setup header also says "Connection lost", since the global bar is
   hidden on this route.
3b. Requirement 14, announcement: the polite live region that announces a
   finished check lives outside the step content, so a check that finishes
   while another step is open is still announced.
4. Completion is recorded, as before, by creating, opening or skipping. Motion
   settles in under 3 seconds and is fully off under reduced motion, which shows
   the final frame. The Storybook smoke test judges every story with reduced
   motion on, so axe checks settled frames; it also runs one normal-motion axe
   pass (English, dark) over the welcome stories, once their finite animations
   have ended.
5. Both themes follow the canvas: the dark artboards on the app's dark tokens,
   the light artboards on the F2.5c light tokens (ADR-0047), with setup's own
   primary button, card shadow and line colour taken from the light artboards.

## Consequences

- No change to `GET /capabilities`, `POST /capabilities/check`, the preload
  bridge, `agentplane.welcome.v1` or any event.
- The e2e demo walks the steps (Next to the report, Check again, the template
  cards, Skip setup) and the create path: a cancelled save dialog stays on
  setup; a chosen file opens the editor, nothing is written before the first
  save, and the saved file is the template with only its id line changed.
