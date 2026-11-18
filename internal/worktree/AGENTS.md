# Worktree isolation guidance

The root `AGENTS.md` applies here. This file adds rules for
`internal/worktree/`, the Git operations behind F3.1 workspace isolation
(ADR-0024). The package creates, snapshots, integrates and removes linked
worktrees. It does not schedule, decide acceptance or emit events: the runtime
owns lifecycle state and reaches this package through a consumer-owned
interface that the CLI composes. A worktree is a separate directory, not a
sandbox.

## Find the owner

| Concern | Start here |
| --- | --- |
| Source/root validation, base commit, Git version, dirty-tracked, submodule and filter-driver rejection | `preflight.go` |
| Root directory, `Create`, `Snapshot`, `Remove`, handle ownership | `worktree.go` |
| Three-way integration with `merge-tree --write-tree`, conflict paths | `integrate.go` |
| Safe ref names under `refs/agentplane/` | `refs.go` |
| Typed, actionable, Git-text-free errors | `errors.go` |
| The single hardened Git runner | `internal/gitstate/run.go` |

## Preserve these contracts

- Start Git only through `gitstate.Run`. Never add a second way to run it, never
  forward the caller's environment, and never put Git's stderr in an error,
  event or terminal message; it is repository-controlled text.
- Refuse, in `Preflight`, repositories that configure `filter.*.smudge|clean|
  process` (including through `include.path`). Checkout executes them even with
  a cleaned environment and hooks disabled, and an unknown driver cannot be
  overridden safely. Report driver names only, never commands. `Create` trusts
  that `Preflight` ran. Hooks stay disabled by the runner.
- Preflight is read-only and creates nothing. It rejects a dirty tracked source
  (worktrees start from HEAD), submodules, bare or missing-HEAD repositories,
  Git older than 2.38, and a worktree root that resolves inside the source.
  Untracked and ignored files are not carried into a worktree; that is a
  documented limitation, not an error.
- Worktree directories are named only from validated run and node-run IDs
  (`[A-Za-z0-9-]{1,64}`), never from graph-controlled text. Refuse an existing
  or symlinked target. The root is created 0700 and an existing root must be a
  real, non-group/other-accessible directory.
- Refs are written only under `refs/agentplane/<run>/` and only through
  `UnitRef`/`IntegratedRef`. A unit ref is a sanitized prefix plus a hash of the
  raw node ID (a ref path component is limited to 255 bytes; enrolled node IDs
  may reach 256). `Snapshot` and `Integrate` validate refs against the exact
  pattern before use.
- `Snapshot` never moves a branch and never touches the source index or working
  tree; it excludes ignored files and rejects nested repositories. `Integrate`
  uses `merge-tree`, so it needs no working tree, never auto-resolves a
  conflict, reports at most 64 sorted paths plus an omitted count, and advances
  the tip with compare-and-swap.
- `Remove` accepts only a live `*Handle` created by the same `Manager`. It never
  takes a path. Refs are never removed: they are the run's deliverable.
- Keep the package free of runtime concepts (nodes, attempts, events). Add them
  in `internal/runtime`, not here.

Read `docs/features/F3.1-workspace-isolation.md`,
`docs/adr/0024-workspace-isolation-with-git-worktrees.md` and
`docs/validation/F3.1-codex-worktree-spike.md` before changing a contract.

## Choose regression tests

- Preflight rejections and read-only behavior: `preflight_test.go`.
- Root permissions, create, hooks, hostile IDs, snapshot, removal and the
  `gitstate` linked-worktree check: `manager_test.go`.
- Integration, conflicts, bounds and determinism: `integrate_test.go`.
- Ref naming against real `git check-ref-format`: `refs_test.go`.
- Runner environment, limits and error classes: `internal/gitstate/run_test.go`.

Tests use temporary Git repositories and a plain-Git oracle (`helpers_test.go`).
Canary files prove that hooks and filter drivers did not run; keep a control that
proves the canary would fire without the protection.
