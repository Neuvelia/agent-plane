# First Codex session - ready to paste

Use this after opening the repository in Codex.

```text
We are starting implementation of AgentPlane.

This is a PLAN-ONLY task. Do not modify files.

Active feature:
F0.1 - Graph Definition Schema v0

Read in this order:
1. AGENTS.md
2. docs/features/F0.1-graph-definition-schema-v0.md
3. docs/architecture.md
4. docs/domain-model.md
5. docs/graph-spec.md
6. docs/testing-strategy.md
7. existing code under internal/graph and cmd/agentplane

Then return:

1. Current state
2. Exact F0.1 scope and explicit non-goals
3. Proposed implementation approach
4. Exact files you would add/change
5. Proposed Go types and package API
6. YAML parsing dependency recommendation, including why it is needed and its trade-offs
7. Unknown-field policy recommendation
8. Test matrix mapped to every acceptance criterion
9. Error-handling strategy
10. Risks and open questions
11. Definition of done, ending with make check

Constraints:
- Do not implement anything yet.
- Do not implement graph semantic validation; that is F0.2.
- Do not implement the scheduler; that is F0.4.
- Do not implement shell execution; that is F0.5.
- Do not add UI/cloud concerns.
- Do not create a template/expression engine.
- Graph parsing itself must not execute commands, expand environment variables, or access the network.

Prefer the smallest API that cleanly supports the next Phase 0 features.
```

After reviewing the plan, if it is good, start a new or continued implementation turn with `prompts/implement.md` and fill:

```text
FEATURE_ID=F0.1
FEATURE_SPEC=docs/features/F0.1-graph-definition-schema-v0.md
```
