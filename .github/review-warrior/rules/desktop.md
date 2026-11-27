---
include:
  - "desktop/**"
---

# Desktop shell

- The desktop UI observes and steers runs through the loopback API only. It never schedules, retries or decides state; logic of that kind in the renderer or Electron main process is a P1.
- The renderer makes no network requests beyond the local sidecar API.
- Electron security: no `nodeIntegration` in the renderer, context isolation stays on, IPC channels validate their input.
- User-visible text goes through the PL/EN localisation; a hard-coded string in one language is a P3.
- Recorded values and diffs are untrusted text: render them as text, never as HTML.
