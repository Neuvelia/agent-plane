---
include:
  - "internal/domain/**"
  - "internal/app/**"
  - "internal/runtime/**"
  - "internal/gate/**"
  - "internal/agent/**"
  - "internal/store/**"
---

# Execution state and events

- Only domain and application code decide state transitions, retries and acceptance (R6). An adapter that starts deciding them is a P1.
- Every execution transition emits a structured event. A transition visible only in a log, or an event persisted after it is delivered (ADR-0027 persist-before-deliver), is a P1.
- An event payload change needs a schema version bump, an ADR and a reader that still accepts the old version. Flag a silent payload change as P1.
- Retry is bounded. A failed unit never reruns an accepted sibling, and accepted output stays immutable for the run.
- Domain and application code never call `time.Now`, `os`, `os/exec`, environment lookups or randomness directly (R4); they take a port.
- Events never carry raw secrets or private model reasoning.
