# Codex prompt - implement approved plan

```text
We are working on AgentPlane.

Active feature:
FEATURE_ID={{FEATURE_ID}}
FEATURE_SPEC={{FEATURE_SPEC}}

Implement the previously approved plan for this feature.

Before editing, re-read:
- AGENTS.md
- {{FEATURE_SPEC}}
- the approved plan
- relevant architecture/ADR files

Rules:
- Do not expand feature scope.
- Keep public contracts explicit and minimal.
- Add or update tests for every acceptance criterion.
- Test failure behavior, not only happy paths.
- Do not add cloud/UI dependencies to the Phase 0 runtime.
- Do not use an LLM for deterministic behavior.
- Treat graph/process/path inputs as untrusted.
- Do not log or serialize secrets.

During implementation:
- run targeted tests as useful;
- preserve useful error causes/context;
- avoid unrelated refactors.

Before finishing:
1. Run `make check`.
2. Inspect `git diff --stat` and the changed files.
3. Confirm no unrelated files changed.
4. Confirm each acceptance criterion is covered.

Final response must contain:
- Summary
- Files changed
- Tests/checks run with actual results
- Acceptance criteria coverage
- Known limitations
- Explicit follow-up work that is outside this feature
```
