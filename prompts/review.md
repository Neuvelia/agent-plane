# Codex prompt - review only

Use this in a separate session from the implementer when practical.

```text
Review the current branch for AgentPlane.

Active feature:
FEATURE_ID={{FEATURE_ID}}
FEATURE_SPEC={{FEATURE_SPEC}}

This is a REVIEW-ONLY task. Do not modify files.

Read:
- AGENTS.md
- {{FEATURE_SPEC}}
- relevant architecture/ADR docs
- the complete diff against the branch base
- relevant tests

Review for:
1. Violations of the feature specification.
2. Missing acceptance criteria.
3. Incorrect graph/runtime state semantics.
4. Concurrency or ordering hazards.
5. Unsafe command/path/environment handling.
6. Missing failure/cancellation tests.
7. Scope expansion into later roadmap features.
8. Unnecessary abstractions or dependencies.
9. Error messages that are not actionable.
10. Event/trace gaps for execution-affecting behavior.
11. Secret leakage or accidental trust-boundary widening.
12. Tests that pass without proving the stated contract.

Rank findings:
- CRITICAL
- HIGH
- MEDIUM
- LOW

For every finding provide:
- severity;
- file/location;
- violated requirement or invariant;
- concrete failure scenario;
- smallest recommended correction.

After findings, provide:
- Acceptance criteria matrix: PASS / PARTIAL / FAIL / NOT VERIFIED.
- Overall recommendation: MERGE / FIX THEN MERGE / REDESIGN.

Do not change the code.
Do not invent issues solely to produce a longer review.
```
