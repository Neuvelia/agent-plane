# Prompt - bounded bug fix

```text
We need to fix a bug in AgentPlane.

Issue: {{ISSUE}}   (or a description)
Bug: {{BUG_DESCRIPTION}}
Expected behavior: {{EXPECTED_BEHAVIOR}}
Feature: {{FEATURE_ID}}   (the registry entry the bug belongs to; find it in
docs/roadmap/features.yaml by searching for the issue number in `tasks`/`issue`)

Do not start with code changes. First:
1. Read AGENTS.md ("Roadmap discipline"), the {{FEATURE_ID}} entry in
   docs/roadmap/features.yaml, docs/roadmap/canonical-spec.md and the entry's ADRs.
2. Reproduce the bug or identify the failing contract (a command, a test, path:line).
3. Find the smallest responsible component.
4. Name the regression test that fails before the fix. If the bug is an open
   acceptance criterion, the test proves that criterion and gets its tag
   (`// roadmap: {{FEATURE_ID}}-ACn`).
5. Propose the smallest fix.

Then implement the fix if the task authorizes it.

Constraints:
- No unrelated refactors.
- Do not change public graph, event, API or CLI semantics unless the bug proves the
  contract wrong. If it does, stop: that needs a spec change and an ADR
  (needs_decision).
- Follow the canonical spec and the security rules in AGENTS.md.

Finish:
- Update docs/roadmap/features.yaml: set the criterion to `met` with evidence,
  remove a closed divergence or gap, and update the feature status if every
  criterion is now met.
- Run `make roadmap`, then `make check`. Report the regression test and the
  actual results.
- Commit `fix(area): [{{FEATURE_ID}}] ... ({{ISSUE}})` and open a PR that
  references the issue ("Fixes {{ISSUE}}" only if the owner wants it closed on
  merge).
```
