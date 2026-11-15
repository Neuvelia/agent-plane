# H13: Gitstate-local agent guidance

## Intent

An agent changing Git-observed state should find the shared snapshot owner,
the two observation scopes and their cross-package regression tests. The
root and Codex/runtime/Gate guidance remain in force at their boundaries.

## Approach

Add `internal/gitstate/AGENTS.md` next to `snapshot.go`. Ground it in
F1.2/ADR-0010, F1.8/ADR-0016 and current tests. Describe controlled Git
subprocesses, workspace/path validation, index and worktree comparisons,
adapter metadata that excludes ignored contents, and the runtime Gate
baseline that includes them.

This changes contributor guidance only. Product code, Git observation
contracts, event schema, dependencies and CI rules remain unchanged.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide distinguishes adapter `SnapshotNow`/`SnapshotAfter` from Gate
  `Capture`/`Changed`, including ignored-path behavior.
- It preserves controlled Git invocation, path confinement, index/worktree
  comparison, deterministic ordering and explicit inspection failures.
- It places baseline lifetime and scope verdicts with runtime/Gate, and
  avoids claiming isolation, prevention, rollback or authorship.
- It names targeted Gitstate, Codex, Gate and runtime tests.
- Focused Gitstate tests and `make check` pass.

## Risks

The adapter and F1.8 Gate intentionally hash different sets of ignored
files. A single simplified rule would mislead future changes. The five-second
Git command and 16 MiB command-output limits do not bound all file hashing
or concurrent workspace activity.
