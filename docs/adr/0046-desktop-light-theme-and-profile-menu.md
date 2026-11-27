# ADR-0046 - Desktop light theme and profile menu

**Status:** accepted
**Date:** 2026-11-27

## Context

F2.5a shipped the desktop renderer in the Tarmind design's dark theme only and
listed "Light theme" among its non-goals. The design canvas now draws a
**Profile menu** (artboards "Profile menu" and "Profile menu · light") under the
avatar at the foot of the sidebar, with an **Appearance** submenu (System,
Light, Dark), a **Language** submenu (System, English, Polski), Settings,
Keyboard shortcuts, Help and documentation, and About. Every screen of the
canvas also has a light variant whose values are given in the canvas's
`tm.css` as "the same token names, light values".

The product owner asked for the profile menu 1:1 with the design, including a
working light theme across the app and the Keyboard shortcuts and About
windows (2026-11-27). That widens F2.5a's scope, so it needs a decision.

## Decision

1. **A second colour theme in the design tokens.** `desktop/src/design/tokens.json`
   gains a `light` theme with a value for every colour token. The values come
   from the canvas's `tm.css` light block where it names the token (bg, panel →
   inset, raised, fg, muted, hairline, fills, brand, ok, bad, warn, the dark
   primary button) and are derived for the rest so that every text pair
   reaches 4.5:1 and every focus/selection indicator 3:1 (checked by
   `desktop/test/contrast.test.ts` for both themes). `scripts/tokens-css.mjs`
   keeps the dark theme on `:root` and emits other themes' colour tokens under
   `:root[data-theme="<id>"]`. Spacing, radius, size and type stay theme-free.
2. **The theme is a renderer preference**, like the language: stored in the
   renderer's `localStorage` (`agentplane.theme`), never sent to the engine or
   the main process, with three choices. `system` (the default, and the only
   value that removes the key) follows `prefers-color-scheme` live; `light`
   and `dark` pin it. The resolved theme is written as `data-theme` on
   `<html>` before the first render, so a saved light choice never flashes dark.
3. **The language preference gains "System"**: choosing it removes the saved
   language (`agentplane.locale`), so the operating system's language applies,
   as it already did on first launch.
4. **The profile moves from the global bar to the foot of the sidebar.** The
   avatar row is a WAI-ARIA menu button; the menu, its two submenus and the two
   windows are presentation only: they call existing navigation, the existing
   read-only `GET /capabilities` and `GET /runs`, and the two preferences above.
   "Help and documentation" stays visible but unavailable ("Soon"), like the
   Docs sidebar entry, until a local documentation surface exists.
5. **Motion** follows the existing rule: every entrance/exit animation and the
   theme cross-fade are skipped under `prefers-reduced-motion`.

## Alternatives considered

### Menu 1:1, Light and System shown as unavailable

Pros:

- No token or contrast work; F2.5a's non-goal stands.

Cons:

- The Appearance submenu would be a control that does nothing; the design's
  light artboards stay unimplemented.

### Theme held by the Electron main process (`nativeTheme.themeSource`)

Pros:

- The window background and native controls could follow before the renderer loads.

Cons:

- Needs a new IPC channel and a main-process preference file for a purely
  visual choice; the language preference already lives in the renderer.
  `color-scheme` on `<html>` gives native controls and scrollbars the right
  look without it.

## Consequences

### Positive

- The canvas's light artboards and the profile menu are implemented; the theme
  choice exists in two places (profile menu and Settings) as drawn.
- Contrast is verified for both themes by the existing unit test and by axe in
  every story, in English and Polish, in both themes (`npm run stories:test`).

### Negative / trade-offs

- `tokens.json` now carries light values the upstream design system does not
  publish. Re-copying the upstream file must keep them (or the light theme
  falls back to dark values and `tokens.test.ts` fails).
- The Electron window background is still black before the renderer paints;
  a light-theme user can see one black frame at start-up.
- Story smoke time doubles (two themes).

## Compatibility and migration

No graph, event, API, run-store or IPC change. Existing installations have no
saved theme, so they start on `system`: on an operating system set to light,
the app now opens light. A saved language is kept; a missing one already
meant "system".

## Validation

- `desktop/test/tokens.test.ts`, `desktop/test/contrast.test.ts` (both themes).
- `desktop/test/profile-menu.test.tsx`, `desktop/test/shell-dialogs.test.tsx`.
- `desktop/scripts/stories-smoke.cjs`: every story in EN/PL × dark/light with
  axe, plus a real-browser check of the menu's keyboard model, focus return,
  submenu alignment, theme switch and reduced motion.
- `desktop/e2e/demo.cjs`: theme switch survives a reload in the real app;
  About and Keyboard shortcuts open and close; Ctrl/Cmd+, opens Settings.
