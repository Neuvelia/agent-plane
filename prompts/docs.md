# Prompt - documentation change

```text
We need a documentation-only change in AgentPlane.

Goal: {{DOCS_GOAL}}
Related feature or issue: {{FEATURE_ID}} / {{ISSUE}}   ("none" if general)

Read AGENTS.md ("Roadmap discipline"), docs/roadmap/README.md,
docs/roadmap/canonical-spec.md and the files you will change. If a feature is
involved, read its entry in docs/roadmap/features.yaml.

Rules:
- Facts come from code and tests (cite path:line in the PR), not from other docs.
  A doc claiming that something is implemented must match the registry. Where they
  disagree, investigate: the registry wins, and if the registry itself is wrong,
  fix it with evidence.
- Change only documentation and docs/roadmap/*.yaml. No product code or tests. A
  tag comment on an existing test is allowed when you add it as evidence.
- Never change the meaning of the canonical spec, a spec's acceptance criteria or
  an ADR's decision without a new ADR. Correcting a stale status line or a factual
  error is fine. Changing scope or a contract is needs_decision.
- Use the canonical vocabulary: `agentplane`, the domain states, snake_case
  event names and the graph format.
- Keep docs/features/README.md, docs/roadmap.md, AGENTS.md "Current product phase"
  and README.md consistent with the registry when you touch the same facts.
- Do not edit docs/roadmap/STATUS.md by hand.

Finish:
- Run `make roadmap`, then `make check`, and report the results.
- Commit `docs(area): [{{FEATURE_ID}}] ...` (omit the ID if there is none). The PR
  lists every corrected statement as old → new, with its source.
```
