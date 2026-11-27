# ADR-0049 - Graph editor v2 layout

**Status:** proposed
**Date:** 2026-11-27
**Features:** F2.5e (graph editor v2). Builds on the light theme of F2.5c (ADR-0047).

## Context

The product owner's "Graph editor v2" design (canvas "Tarmind Dashboard UX",
artboards *Graph editor v2: step cards* light and dark, *error blocks run*, *graph
menu*, *step menu*) replaces the F2.1 editor layout (cards stacked below a toolbar of
buttons) with one header row, a full-bleed canvas of self-describing step cards, an
inspector on the right and problems in a status bar that opens a drawer. The light
and dark themes come from F2.5c (ADR-0047).

Several controls in the design have no engine or file-grant support: testing one
step, running from a step, canvas comments, copying or duplicating a step, and
renaming, duplicating, revealing, copying the path of or deleting the graph file.
The renderer only knows a granted file by its name (ADR-0021).

## Decision

1. **New colour tokens** for the editor v2, each with a dark and a light value in
   the F2.5c token set: `line`, `brand-text`, `primary-bg/fg/hover` (one flat primary
   per screen; dark-on-light so it does not fade on a white ground), `ring`,
   `elevation`, `elevation-menu`, `scrim`.
2. **Editor layout v2** is presentation only. Document state, history, conflict
   handling, the engine's diagnostics, operations and run gating are unchanged
   (`doc.ts`, `useGraphDocument.ts`, ADR-0020/0021). A step card summarises a node
   from the definition, the sidecar's field descriptors and the diagnostics
   (`cardModel.ts`); it never branches on a node type to decide anything.
3. **Unsupported design controls are shown, disabled and tagged "Soon"** (owner
   decision, 2026-11-27). They are `aria-disabled`, do nothing and send nothing.
   Each needs its own feature and contract before it works.
4. **Storybook** gains stories for each artboard state; the F2.5c smoke requires
   them in both languages and themes and runs axe on open menus too.

## Alternatives considered

### Hide unsupported controls

Pros: no control that does nothing.
Cons: diverges from the design the owner approved; hides planned capability.

### Implement the missing actions now

Pros: every control works.
Cons: new engine operations and file-grant endpoints (rename, delete, reveal) change
the trust boundary of ADR-0021 and need their own specs; out of scope here.

## Consequences

### Positive

- The graph reads without opening anything: each card shows its prompt, key
  settings, where its inputs come from and what it puts out.

### Negative / trade-offs

- Each new colour token needs a light value.
- "Soon" controls are visible before they work.
- Node ids, Open, New and Close moved into menus; the e2e script and stories were
  updated to reach them the way a person does.

## Compatibility and migration

No change to graphs, events, storage, the sidecar API or the preload bridge. The
editor's remembered view and drawer state are new `localStorage` keys
(`agentplane.editor.view`, `agentplane.editor.problems`); a
missing or unreadable value falls back to the default.

## Validation

`npm run check` (token parity, unit tests, builds), `make desktop-stories` (every
story in EN/PL x dark/light, axe including open menus, play completed) and
`make desktop-e2e` (real Electron and sidecar through the v2 editor).
