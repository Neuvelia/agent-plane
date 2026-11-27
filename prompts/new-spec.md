# Prompt - write a feature spec

Use this before a registry entry without a spec (for example F3.3, F3.5, F3.6,
F3.7, F3.10, F3.11, F1.4b) is activated.

```text
Write the feature spec for {{FEATURE_ID}} in AgentPlane. Documentation only; do not
implement.

Read:
1. AGENTS.md ("When a feature spec is required", "Roadmap discipline").
2. The {{FEATURE_ID}} entry in docs/roadmap/features.yaml. Its acceptance criteria
   are copied 1:1 from the roadmap; its out_of_scope, depends_on, decisions and
   divergences also apply.
3. The {{FEATURE_ID}} section of docs/reference/full-roadmap-features.md: the
   purpose ("Po co"), the functional scope ("Zakres funkcjonalny"), the
   acceptance criteria ("Kryteria akceptacji") and the out-of-scope line
   ("Poza zakresem").
4. docs/roadmap/canonical-spec.md, the ADRs it depends on, the specs of its
   dependencies, and the existing code that the feature touches. Cite
   path:line for current state.
5. The GitHub issues in the entry's `tasks`.

Write docs/features/{{FEATURE_ID}}-<slug>.md from docs/features/_template.md:
- Status `draft`.
- A "Roadmap criteria" section listing every roadmap criterion ID
  ({{FEATURE_ID}}-AC1...) with its text. Then the spec's own measurable criteria,
  each mapped to the roadmap criterion it proves. A roadmap criterion that the
  spec narrows, redefines or defers must say so explicitly and name the ADR or the
  target feature ID; no silent narrowing.
- Non-goals: quote the entry's out_of_scope. Every deferral names a target feature ID.
- Contracts affected; whether an ADR is needed (AGENTS.md "When an ADR is
  required"). If yes, draft it as `proposed` with scripts/new-adr.sh and add it to
  docs/roadmap/decisions/README.md when it settles a roadmap question.
- Failure behavior, security/privacy impact, observability (events), retry and
  recovery behavior of side effects, and the required tests (where the
  `// roadmap:` tags will go).
- Open questions as needs_decision items.

Registry: set the entry's `spec:` path; keep its status (`not_started`) unless work
starts. Add open questions to `needs_decision:` in docs/roadmap/features.yaml.
Run `make roadmap` and `make check`. Commit `docs(spec): [{{FEATURE_ID}}] ...`.
```
