# Prompt - plan only

Replace placeholders before use. One feature (or one named slice) per session.

```text
We are working on AgentPlane. This is a PLAN-ONLY task. Do not modify files.

Active feature: {{FEATURE_ID}}
Spec: {{FEATURE_SPEC}}   (write "none" if the entry has no spec yet; then use prompts/new-spec.md first)

Read, in this order:
1. AGENTS.md (including "Roadmap discipline").
2. The entry for {{FEATURE_ID}} in docs/roadmap/features.yaml: status, acceptance
   criteria (IDs and status), out_of_scope, depends_on, decisions, divergences, tasks.
3. docs/roadmap/canonical-spec.md and every ADR listed in the entry's `decisions`.
4. {{FEATURE_SPEC}} and docs/architecture.md, docs/domain-model.md,
   docs/graph-spec.md where relevant.
5. The code and tests the entry's evidence points to, and the code you would change.
6. docs/roadmap/STATUS.md for the state of dependencies.

Return exactly these sections:

1. Current state: what exists, with path:line; which criteria are met or open
   according to the registry, and whether the code agrees.
2. Scope: in scope; out of scope (quote the entry's out_of_scope and the spec's
   non-goals); dependencies that are not done.
3. Proposed implementation: minimal steps for this feature only.
4. Files to change, with the reason for each.
5. Contracts affected: graph schema, events (schema version), API, CLI, persistence,
   canonical spec. Say which need an ADR (AGENTS.md "When an ADR is required").
6. Criterion → test map: for every acceptance criterion ID, the test(s) that will
   prove it, including failure cases, and where the `// roadmap:` tag goes.
7. Registry update you will make: the criterion statuses, the feature status,
   divergences (each with adr, issue or deferred_to) and tasks.
8. Risks: concurrency, compatibility, security, process lifecycle, scope.
9. needs_decision: every ambiguity or conflict with the spec, the canonical spec
   or the roadmap. Do not choose silently. An issue opened for one of them is
   `needs-owner` (docs/roadmap/issue-triage.md).
10. Definition of done: the checks you will run (make roadmap, make check, and
    narrower ones).

Do not implement anything. Do not plan later roadmap features unless a minimal
compatibility seam is required, and name it as such.
```
