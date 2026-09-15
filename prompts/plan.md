# Codex prompt - plan only

Replace placeholders before use.

```text
We are working on AgentPlane.

Active feature:
FEATURE_ID={{FEATURE_ID}}
FEATURE_SPEC={{FEATURE_SPEC}}

This is a PLAN-ONLY task. Do not modify files.

First read:
- AGENTS.md
- {{FEATURE_SPEC}}
- docs/architecture.md
- docs/domain-model.md
- docs/graph-spec.md when relevant
- relevant ADRs
- relevant existing code/tests

Then return exactly these sections:

1. Current state
   - What exists now that is relevant to the feature?

2. Scope interpretation
   - What is in scope?
   - What is explicitly out of scope?

3. Proposed implementation
   - Step-by-step technical approach.
   - Keep it minimal for the active feature.

4. Files to change
   - Existing files to edit.
   - New files to add.
   - Explain why each file is needed.

5. Contracts affected
   - Graph schema/API/runtime state/events/CLI/persistence, as applicable.

6. Tests
   - Map each acceptance criterion to one or more tests.
   - Include important failure cases.

7. Risks
   - Concurrency, compatibility, security, process lifecycle, or scope risks.

8. Open questions
   - Call out specification ambiguity rather than silently choosing a large architectural direction.

9. Definition of done
   - Concrete final checks, including make check.

Do not implement anything in this session.
Do not add future roadmap features unless required as a minimal compatibility seam.
```
