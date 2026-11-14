# H1: Local task journal

## Intent

Long coding-agent tasks need a small, recoverable record of what remains after
context compaction or a handoff. This implements the task-journal idea in the
[OpenBot repository guidance](https://github.com/nightly-labs/openbot/blob/main/AGENTS.md)
for AgentPlane's existing worktree and review workflow.

## Design

Use one `TASKS.md` at the root of each worktree. The repository currently tracks
that name as a historical implementation queue, so first move its complete
contents to `docs/reference/implementation-queue.md` and update the README
reference. Then ignore only the root journal in Git. For work with more than a
few steps, the implementer records the objective,
the active branch, links to the spec or plan, a short checklist, the last
verification result, and the next concrete action. Update it after a meaningful
step and before a handoff; read it when resuming. Do not put secrets, raw model
output, or sensitive repository content in the journal. The journal is local
working state, not the durable feature spec or final report.

## Approaches

- **Root `TASKS.md` per worktree (chosen):** follows the active checkout,
  requires no service or new tool, and stays out of commits. The existing
  tracked queue moves to the reference docs without dropping its history.
- **Committed task log:** reviewable history, but generates noise and can become
  stale after the PR merges.
- **External tracker:** useful for team coordination, but adds a service and
  loses the worktree-local resume point.

## Scope and acceptance

- `AGENTS.md` and `docs/development-workflow.md` describe when to create,
  update, read, and omit the journal without duplicating the implementation plan.
- The tracked queue's contents remain in `docs/reference/implementation-queue.md`.
- `/TASKS.md` is ignored; nested files with that name are not newly ignored.
- No product runtime, graph, API, event, or CI contract changes.
- `git check-ignore -v TASKS.md` identifies the repository rule, the working
  tree remains clean with a local journal, and `make check` passes.

## Risks

The journal may be stale or contain secrets. Require updates at meaningful
transitions and before handoff, and explicitly exclude secret values and raw
execution material. The journal does not grant process or filesystem isolation.
