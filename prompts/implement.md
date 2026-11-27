# Prompt - implement an approved plan

```text
We are working on AgentPlane. Implement the approved plan for one feature.

Active feature: {{FEATURE_ID}}
Spec: {{FEATURE_SPEC}}
Approved plan: {{PLAN}}   (a path, or "the plan above")
Branch: {{BRANCH}}

Before editing, re-read AGENTS.md ("Roadmap discipline", "Definition of done"),
the {{FEATURE_ID}} entry in docs/roadmap/features.yaml, docs/roadmap/canonical-spec.md,
the ADRs in the entry's `decisions`, the spec and the plan. Keep a TASKS.md
journal for multi-step work (AGENTS.md, Step 2).

Rules:
- Implement only the plan and the entry's scope; respect out_of_scope. If the plan
  needs a contract or scope change that has no ADR, stop and report it as
  needs_decision instead of editing the spec or the canonical spec silently.
- Follow the canonical spec: CLI `agentplane`, snake_case events, the domain
  state enums, the graph format and the adapter interface.
- For every acceptance criterion you prove, add or extend a test and tag it:
  `// roadmap: {{FEATURE_ID}}-ACn` directly above a Go test function,
  `[{{FEATURE_ID}}-ACn]` in a vitest `it()` name, or a `// roadmap:` comment
  in desktop/e2e/demo.cjs.
- Test failure behavior, not only happy paths. No sleeps for synchronization.
- Local-first; no LLM for deterministic work; treat graph, process, path and
  environment inputs as untrusted; never log or serialize secrets; emit events
  for every execution-affecting transition.
- No unrelated refactors.

Registry update (docs/roadmap/features.yaml), in the same change:
- Set each criterion `met` only with evidence ("path::TestName"); otherwise leave
  it `open`, with `issue`/`note` when a gap remains.
- Set the feature status by the rules in docs/roadmap/README.md: `done` only if
  every criterion is met.
- Record divergences (adr, issue or deferred_to) and add issue numbers to `tasks`.
- If you close a GitHub issue's gap, say so in the report. Do not close the issue
  yourself unless asked.

Before finishing:
1. Run `make roadmap` (regenerates docs/roadmap/STATUS.md), then `make check`.
   Run narrower tests first when useful; desktop changes also need
   `cd desktop && npm run check`.
2. Inspect `git diff --stat` and every changed file. Confirm no unrelated files
   changed and no TASKS.md, binaries or temporary files are staged.
3. Commit with the feature ID (`feat(area): [{{FEATURE_ID}}] ...`), push and open
   a PR titled `[{{FEATURE_ID}}] ...` using .github/PULL_REQUEST_TEMPLATE.md.

Final report:
- summary of the behavior;
- files changed;
- checks with their actual results;
- a criterion matrix (ID → met/open → evidence);
- the registry changes;
- known limitations;
- follow-up work outside this feature (with issue numbers if you created any).
  Label every issue you create `claude-ready` or `needs-owner` by
  docs/roadmap/issue-triage.md.
```
