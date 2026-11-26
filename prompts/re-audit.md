# Prompt - roadmap re-audit

```text
Re-audit AgentPlane against its roadmap registry. Read-only for product code and
tests; you may change only docs/roadmap/ (the new audit, features.yaml,
phases.yaml, STATUS.md) and fix stale documentation statements that the registry
already settles.

Previous audit: {{PREVIOUS_AUDIT}}   (the newest docs/roadmap/audit/AUDIT-*.md)
Today: {{DATE}}

Follow "Re-audit procedure" in docs/roadmap/README.md:
1. Baseline: read the previous audit, features.yaml, phases.yaml,
   canonical-spec.md and AGENTS.md. Run `make roadmap`, `make check` and
   `git log --since=<date of the previous audit> --oneline`.
2. Registry vs code: for every feature that is not done or not_started, and every
   feature touched since the previous audit, check each criterion against the
   code and tests. Status comes from evidence (a tagged test), never from spec
   text. Record path:line.
3. Canonical spec vs code: the CLI usage string (cmd/agentplane/cli.go), the
   state enums (internal/domain/run/state.go), the event types and the current
   schema version (internal/domain/events), the graph fields (docs/graph-spec.md,
   internal/adapter/yamlgraph) and the adapter interface (internal/agent/contract.go).
4. Invariants and cross-cutting requirements (canonical-spec.md §8, roadmap §5):
   - secrets in logs, events or exports;
   - idempotency;
   - scheduler determinism;
   - no scheduler semantics in desktop/;
   - no endpoint that runs an arbitrary command;
   - unknown is not zero.
5. Docs vs registry: spec status lines, docs/roadmap.md, docs/features/README.md,
   AGENTS.md "Current product phase", README.md.

Write docs/roadmap/audit/AUDIT-{{DATE}}.md (Polish, like the previous audits):
- a summary: the phase de facto, P0 progress per phase (from STATUS.md) and the top
  risks;
- "Zmiany od poprzedniego audytu": what improved, what newly diverged, what is
  unchanged. Use the feature tables of both audits and
  `git diff <commit of the previous audit> -- docs/roadmap/features.yaml`;
- findings with path:line, separated into Doc↔Doc, Doc↔Repo and Repo-only;
- invariant violations;
- needs_decision items, also added to `needs_decision:` in features.yaml.

Update features.yaml only where the evidence supports it (with tags), regenerate
STATUS.md and run `make check`. Open a PR titled `[roadmap] audit {{DATE}}`. Do not
merge, and do not change product code or tests.
```
