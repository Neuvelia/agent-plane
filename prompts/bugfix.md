# Codex prompt - bounded bug fix

```text
We need to fix a bug in AgentPlane.

Bug:
{{BUG_DESCRIPTION}}

Expected behavior:
{{EXPECTED_BEHAVIOR}}

Do not start with code changes.

First:
1. Read AGENTS.md.
2. Reproduce or identify the failing contract.
3. Find the smallest responsible component.
4. Propose the smallest fix.
5. Identify the regression test that should fail before the fix.

Then implement the fix if the task authorizes implementation.

Constraints:
- Do not refactor unrelated code.
- Do not change public graph/event semantics unless the bug proves the contract itself is wrong.
- If a public contract must change, stop and call out the need for a feature spec/ADR.

Finish with `make check` and report the regression test explicitly.
```
