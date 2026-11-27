# ADR-0046 - Desktop light theme and graph editor v2 layout

**Status:** proposed
**Date:** 2026-11-27
**Features:** F2.5b (graph editor v2, light theme); amends F2.5a non-goal "Light theme"

## Context

F2.5a put the desktop on the Tarmind design in a dark theme only and listed a light
theme as a non-goal. The product owner's "Graph editor v2" design (canvas "Tarmind
Dashboard UX", artboards *Graph editor v2: step cards* light and dark, *error blocks
run*, *graph menu*, *step menu*) is light-first, adds a dark variant, and replaces the
F2.1 editor layout (cards stacked below a toolbar of buttons) with one header row, a
full-bleed canvas of self-describing step cards, an inspector on the right and
problems in a status bar that opens a drawer.

Several controls in the design have no engine or file-grant support: testing one
step, running from a step, canvas comments, copying or duplicating a step, and
renaming, duplicating, revealing, copying the path of or deleting the graph file.
The renderer only knows a granted file by its name (ADR-0021).

## Decision

1. **Two themes.** `desktop/src/design/tokens.json` holds a `light` value for every
   colour token beside `dark`. The generator writes the dark values to `:root` and the
   light values to `:root[data-theme="light"]`. A per-viewer preference (System,
   Light, Dark; default System) lives in renderer `localStorage` like the language and
   is resolved against `prefers-color-scheme`. Nothing reaches the sidecar.
2. **New colour tokens** for the editor v2: `line`, `brand-text`, `primary-bg/fg/hover`
   (one flat primary per screen; dark-on-light so it does not fade on a white ground),
   `ring`, `elevation`, `elevation-menu`, `scrim`, `avatar`, `switch-off`.
3. **Editor layout v2** is presentation only. Document state, history, conflict
   handling, the engine's diagnostics, operations and run gating are unchanged
   (`doc.ts`, `useGraphDocument.ts`, ADR-0020/0021). A step card summarises a node
   from the definition, the sidecar's field descriptors and the diagnostics
   (`cardModel.ts`); it never branches on a node type to decide anything.
4. **Unsupported design controls are shown, disabled and tagged "Soon"** (owner
   decision, 2026-11-27). They are `aria-disabled`, do nothing and send nothing.
   Each needs its own feature and contract before it works.
5. **Storybook** gains a theme toolbar; the browser smoke renders every story in
   English and Polish (dark) and English (light), and fails a story whose play
   function throws or never finishes.

## Alternatives considered

### Dark only, editor v2 layout only

Pros: no token work, smaller change.
Cons: the design is light-first; the light artboards could not be reproduced.

### Hide unsupported controls

Pros: no control that does nothing.
Cons: diverges from the design the owner approved; hides planned capability.

### Implement the missing actions now

Pros: every control works.
Cons: new engine operations and file-grant endpoints (rename, delete, reveal) change
the trust boundary of ADR-0021 and need their own specs; out of scope here.

## Consequences

### Positive

- The desktop follows the operating system's appearance; both themes pass the
  4.5:1 text contrast test (`desktop/test/contrast.test.ts`) and axe in Storybook.
- The graph reads without opening anything: each card shows its prompt, key
  settings, where its inputs come from and what it puts out.

### Negative / trade-offs

- A light value must be chosen for every new colour token.
- "Soon" controls are visible before they work.
- Node ids, Open, New and Close moved into menus; the e2e script and stories were
  updated to reach them the way a person does.

## Compatibility and migration

No change to graphs, events, storage, the sidecar API or the preload bridge. The
editor's remembered view and drawer state are new `localStorage` keys
(`agentplane.editor.view`, `agentplane.editor.problems`, `agentplane.theme`); a
missing or unreadable value falls back to the default.

## Validation

`npm run check` (token parity, contrast for both themes, unit tests, builds),
`make desktop-stories` (every story x EN/PL dark + EN light, axe, play completed) and
`make desktop-e2e` (real Electron and sidecar through the v2 editor).
