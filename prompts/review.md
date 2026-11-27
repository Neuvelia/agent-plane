# Prompt - review only

Use this in a separate session from the implementer.

```text
Review the current branch of AgentPlane. This is a REVIEW-ONLY task. Do not
modify files.

Feature: {{FEATURE_ID}}
Spec: {{FEATURE_SPEC}}
Base: {{BASE_BRANCH}}

Read AGENTS.md, the {{FEATURE_ID}} entry in docs/roadmap/features.yaml (as on the
branch and as on the base), docs/roadmap/canonical-spec.md, the entry's ADRs,
the spec, the complete diff against the base and the relevant tests.

Review for:
1. Violations of the spec, the canonical spec or an ADR. A contract or scope change
   without an ADR is a finding.
2. Registry honesty:
   - a criterion set `met` whose evidence test does not prove it;
   - `done` with weak evidence;
   - a missing `// roadmap:` tag;
   - a divergence without adr, issue or deferred_to;
   - a status that disagrees with the rules in docs/roadmap/README.md;
   - a stale STATUS.md.
3. Acceptance criteria not covered, or covered only on the happy path.
4. Graph or runtime state semantics, concurrency and ordering hazards.
5. Unsafe command, path or environment handling; secret leakage; widening of a
   trust boundary.
6. Missing failure, cancellation or recovery tests.
7. Scope expansion beyond out_of_scope, or later roadmap features implemented
   implicitly.
8. Roadmap invariants (AGENTS.md "Roadmap discipline") and the roadmap
   Definition of Done.
9. Event or trace gaps for execution-affecting behavior.
10. Unnecessary abstractions or dependencies; clean-code and architecture rules
    (R1-R9).
11. Error messages that are not actionable.
12. Tests that pass without proving the stated contract.

Rank the findings CRITICAL / HIGH / MEDIUM / LOW. For each give:
- the severity;
- path:line;
- the violated requirement (criterion ID, ADR, rule);
- a concrete failure scenario;
- the smallest correction.

Then give:
- a criterion matrix: ID → PASS / PARTIAL / FAIL / NOT VERIFIED, and whether the
  registry status matches;
- the result of `go run ./scripts/roadmapcheck`, if you can run it;
- a recommendation: MERGE / FIX THEN MERGE / REDESIGN.

Do not change code. Do not invent issues to lengthen the review. If you are asked
to file findings as issues, label each `claude-ready` or `needs-owner` by
docs/roadmap/issue-triage.md.
```
