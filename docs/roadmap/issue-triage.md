# Issue triage

Every open issue carries exactly one **class label**. The class says whether an
agent may take the issue without the owner. The rules below are the only source;
`AGENTS.md`, the prompts in `prompts/` and the issue forms point here.

| Label | Meaning |
| --- | --- |
| `claude-ready` | A bounded task an agent (Claude Code, Codex) may pick up, implement and open a PR for without asking the owner first. The PR is still reviewed and never merged by the agent. |
| `needs-owner` | The owner has to decide, provide an environment or shape the work first. An agent may plan, comment or split it, but does not implement it on its own. |
| `triage` | Not classified yet. Set and cleared automatically. |

`needs_decision` keeps its meaning (an open architectural or product decision, also
listed in `features.yaml` `needs_decision:`) and always comes with `needs-owner`.

## Rule

An issue is `claude-ready` only when **all** of these hold. When in doubt, it is
`needs-owner`; the owner can promote it.

1. **One feature.** It belongs to one registry entry in `docs/roadmap/features.yaml`
   (name the ID in the title, `[F2.5b] ...`) and stays inside that entry's
   `out_of_scope`.
2. **Unambiguous outcome.** The expected behavior is stated: an open acceptance
   criterion, reproduction steps with the expected result, or a concrete
   `path:line` with the required change. No choice between designs is left open.
3. **No ADR.** It does not change a public or durable contract: the graph schema,
   event types or schema version, the loopback API, the CLI, persistence, the
   canonical spec, or anything listed in AGENTS.md "When an ADR is required".
4. **Deterministic proof.** A test can prove it (a regression test, a tagged
   criterion test, a vitest), and `make check` (plus `cd desktop && npm run check`
   for desktop work) is enough to verify it.
5. **Runs in an agent container.** It needs no real agent CLI login, no
   macOS/Windows machine, no secrets or API keys, and no manual UI judgment
   (visual design or copy that the owner has not specified).
6. **No trust boundary.** It does not touch workspace or process isolation,
   secret handling, permissions, listeners, or what leaves the machine.
7. **One PR.** It fits one reviewable PR in one area, roughly up to 400 changed
   lines including tests.

These labels make an issue `needs-owner` regardless of the rest, and the
`issue-labels` workflow removes `claude-ready` next to them: `needs_decision`,
`epic`, `spike`, `feature` (a whole roadmap feature with a spec).

## Who labels

- **Agents** that open an issue (follow-ups from `prompts/implement.md`,
  `prompts/bugfix.md`, `prompts/review.md`, gaps from `prompts/re-audit.md`) apply
  the rule and set `claude-ready` or `needs-owner` when creating it. A
  `claude-ready` issue body states the expected outcome, the feature ID and how to
  verify it, so the agent that takes it later needs nothing else. A `needs-owner`
  body ends with an **Owner decisions** section (below).
- **The owner** chooses in the issue form's "Autonomy" dropdown, or sets the label
  by hand. Switching the class is adding the other label: the workflow removes
  the old one.
- **Backfill or re-check** of existing issues uses `prompts/triage.md`. It
  proposes; the owner approves before labels change.

## Owner decisions section

Every `needs-owner` issue lists what the owner has to decide or provide, as task
list checkboxes the owner ticks directly in the issue. One block per decision,
numbered `D1`, `D2`, ...; one checkbox per option; the recommended option first
and marked. Environment or review needs (a macOS machine, an API key, a design)
are a decision with the options the owner can choose from.

```markdown
## Owner decisions

<!-- Tick one option per decision, or comment with another answer. -->

**D1. What does `#` match in the command palette?**
- [ ] A: run ID prefix only, as in the spec (recommended; no ADR)
- [ ] B: run ID prefix or graph name (spec change, needs an ADR)

**When decided:** with A the issue becomes `claude-ready`; with B an ADR comes first.
```

The last line says what follows the decision: the issue becomes `claude-ready`,
is split, needs an ADR or a spec, or is closed. After ticking, the owner swaps
the label (or asks an agent to). An agent that takes the issue reads the ticked
options as the decision; an unticked decision means the issue is not ready.

For issues that existed before this rule, the section is posted as the first
comment by the triage backfill instead of rewriting the body.

## Enforcement (`.github/workflows/issue-labels.yml`)

The workflow runs on `issues` events (`opened`, `labeled`, `unlabeled`) and is
deterministic (`.github/scripts/issue-labels.cjs`, tested by
`node --test .github/scripts/issue-labels.test.cjs`). It does not judge the
rule above; it keeps the labels consistent:

- creates `claude-ready`, `needs-owner` and `triage` when they are missing;
- on `opened`, takes the form's "Autonomy" choice when no class label is set;
- a blocking label removes `claude-ready` and adds `needs-owner`;
- with both class labels, the one added by the current event wins; otherwise
  `needs-owner` wins;
- `triage` is present exactly when neither class label is.

## Taking a `claude-ready` issue

Use `prompts/bugfix.md` (a bug) or `prompts/implement.md` (a task under an
approved plan or an open criterion) with the issue as `{{ISSUE}}`. If the work
turns out to need a decision, stop, comment on the issue with what is missing,
and swap the label to `needs-owner` instead of choosing silently.
