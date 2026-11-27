# Reusable prompts

Prompts for agent sessions (Codex, Claude Code or another agent). Each one
follows the roadmap harness described in `docs/roadmap/README.md` (ADR-0038).
Replace the `{{PLACEHOLDERS}}` and paste the fenced block as the first message.

| Prompt | Use for | Changes files? |
| --- | --- | --- |
| [`plan.md`](plan.md) | planning one feature (or one slice) before implementation | no |
| [`implement.md`](implement.md) | implementing an approved plan | yes |
| [`bugfix.md`](bugfix.md) | a bounded bug fix, usually a GitHub issue | yes |
| [`docs.md`](docs.md) | documentation-only changes (specs, docs, registry) | docs only |
| [`new-spec.md`](new-spec.md) | writing a feature spec for a registry entry before it is activated | docs only |
| [`review.md`](review.md) | reviewing a branch in a separate session | no |
| [`triage.md`](triage.md) | classifying issues as `claude-ready` / `needs-owner` (`docs/roadmap/issue-triage.md`) | labels, after approval |
| [`re-audit.md`](re-audit.md) | re-auditing the repo against the registry and canonical spec | report + registry |
| [`refactor-clean-architecture.md`](refactor-clean-architecture.md) | the layered refactor plan (historical) | – |
| [`first-session.md`](first-session.md) | historical F0.1 kickoff | – |

Common placeholders:

| Placeholder | Example |
| --- | --- |
| `{{FEATURE_ID}}` | `F1.4b`; see `docs/roadmap/features.yaml` |
| `{{ISSUE}}` | `#178` |
| `{{FEATURE_SPEC}}` | `docs/features/F1.7-return-envelope-scoped-retry.md` |

Every prompt shares the **harness contract**:

1. Read the registry entry, `docs/roadmap/canonical-spec.md`, the linked ADRs and
   the spec.
2. Stay inside `out_of_scope`. A scope or contract change needs an ADR, never a
   silent edit.
3. Tag the tests that prove criteria (`// roadmap: Fx.y-ACn`).
4. Update `evidence`, the statuses, `divergences` and `tasks` in
   `docs/roadmap/features.yaml`.
5. Run `make roadmap` and `make check`, and report the actual results.
6. Put the feature ID in commits and the PR (`[Fx.y] ...`).
7. Label every issue you open `claude-ready` or `needs-owner`
   (`docs/roadmap/issue-triage.md`).
