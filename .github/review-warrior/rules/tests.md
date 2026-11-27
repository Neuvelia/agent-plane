---
include:
  - "**/*_test.go"
  - "desktop/**/*.test.ts"
  - "desktop/**/*.test.tsx"
  - "desktop/e2e/**"
---

# Tests

- A test proves a contract, not an implementation detail. Flag a test that passes even when the behaviour it names is broken.
- No sleeps for synchronization; concurrency tests prove overlap or ordering with explicit barriers or channels.
- New behaviour needs a failure-path test, not only the happy path.
- Fakes are hand-written implementations of ports; no mocking frameworks.
- Fixtures contain no real secrets, tokens or personal data.
