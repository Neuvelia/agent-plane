# ADR-0046 - Welcome and setup as a four-step setup

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
   "Open a graph I already have…".
3. The last step's primary action creates the picked graph directly (the same
   `newGraph` grant and template draft as the F2.12 picker); it no longer opens
   the picker. The picker stays the editor's "New graph" flow.
4. Completion is recorded, as before, by creating, opening or skipping. Motion
   settles in under 3 seconds and is fully off under reduced motion, which shows
   the final frame. The Storybook smoke test judges every story with reduced
   motion on, so axe checks settled frames.
5. The canvas's light variant is not implemented: the app is dark only
   (F2.5a non-goal). A light theme needs its own feature.

## Consequences

- No change to `GET /capabilities`, `POST /capabilities/check`, the preload
  bridge, `agentplane.welcome.v1` or any event.
- The e2e demo walks the steps: Next to the report, Check again, the template
  cards, Skip setup.
