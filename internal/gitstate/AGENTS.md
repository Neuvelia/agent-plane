# Git-observed workspace guidance

The root `AGENTS.md` applies here. This file adds rules for
`internal/gitstate/`, the provider-neutral Git snapshot helper used by the
Codex adapter and the opt-in changed-files scope Gate. It observes local
state; it does not run a graph, decide a Gate verdict or isolate a workspace.

## Find the owner

| Concern | Start here |
| --- | --- |
| Controlled Git invocation (shared with `internal/worktree`) | `run.go`: `Run`, `RunOptions`, `RunError` |
| Workspace root and HEAD checks | `snapshot.go`: `git`, `CanonicalWorkspace` |
| Index/working-file snapshots, ignored inventory and path confinement | `snapshot.go`: `snapshotAfter`, `ignoredInventory`, `ConfinedPath` |
| Runtime-owned scope baseline including ignored files | `snapshot.go`: `Capture`, `Changed` |
| Adapter metadata snapshot excluding ignored contents | `snapshot.go`: `SnapshotNow`, `SnapshotAfter`, `IgnoredInventory`, `ChangedSnapshots` |
| Scope baseline lifetime and Gate verdict | `internal/runtime/loop.go`, `internal/gate/changed_files.go` |
| Codex adapter evidence and error translation | `internal/agent/codex/git.go` |

## Preserve these contracts

- Require an absolute workspace that resolves to the exact root of an
  initialized, non-bare Git worktree with a committed HEAD. `git()` delegates
  to the exported `Run`, the only way this repository starts Git: it calls
  the `git` found on PATH (`gitlocate.ResolveGit`, `ErrGitNotFound` when absent; relative results refused, macOS shim guarded) with structured arguments, a controlled environment (`Environment`),
  disabled optional locks/fsmonitor and no configured hooks or global/system
  config. By default each Git subprocess has a five-second context bound and a
  16 MiB stdout bound (callers such as `internal/worktree` pass larger
  timeouts through `RunOptions`); `Run` exposes only `Write` to the process so
  `io.Copy` cannot bypass the stdout limit through `bytes.Buffer.ReadFrom`.
  Errors are `*RunError` with a class and exit code, never Git's stderr. File hashing reads workspace contents separately; these Git
  subprocess limits are not a global bound on files, memory or inspection
  time. Do not emit subprocess stderr as an event; callers own redaction and
  size limits when they expose validated paths as evidence.
- Compare both index entries and working-file existence, mode and content
  digests against a pre-execution snapshot, not just HEAD. Preserve unchanged
  pre-existing dirt as unchanged; further edits count. Return unique sorted
  paths. A rename appears as old-path removal and new-path addition.
  Reject unresolved index stages, submodules and unsupported filesystem
  entries. Hash a symlink's target text without following the final symlink.
- `ConfinedPath` rejects absolute, empty, invalid UTF-8 and NUL paths, dot
  or parent segments, `.git` in any letter case, and symlink or non-directory
  ancestors. Validate each listed path before inspection; do not replace
  this with string prefix matching. These checks handle untrusted Git paths
  but do not make concurrent workspace writes safe or provide a filesystem
  sandbox.
- Preserve two different observation scopes. `SnapshotNow`/
  `SnapshotAfter` serve adapter metadata: tracked and nonignored untracked
  paths are hashed, initially ignored contents are not read, and
  `IgnoredInventory` records bounded names/prefixes. Reobserve baseline
  paths even if they later become ignored. If a newly visible path was
  initially ignored, fail because its old content is unknown. `Capture`/
  `Changed` serve the F1.8 Gate: include ignored files so out-of-scope writes
  beneath ignore rules are visible. `Baseline.Head` is diagnostic context;
  the change list comes from snapshot comparison, not a HEAD-only diff.
- `gitstate` returns observations and errors, never a green/red scope
  decision. Runtime owns when the Gate baseline is captured and retains a
  successfully captured baseline across correction attempts; the Gate owns
  path rules, warn/deny results and bounded evidence. A failed inspection
  must not silently become an empty change list. The observations show
  differences during the interval, not which process authored them.
  Concurrent Shell or external writers can
  affect results. Scope checks happen after execution and neither prevent
  writes nor roll them back.

Read `docs/adr/0010-codex-process-and-observed-evidence.md` and
`docs/features/F1.2-first-coding-agent-adapter.md` for adapter evidence.
Read `docs/adr/0016-changed-files-scope-check.md` and
`docs/features/F1.8-changed-files-scope-check.md` for runtime Gate scope.
A change to snapshot meaning, ignored-path handling or path trust rules
affects both consumers and requires a feature spec and ADR where root policy
requires one.

## Choose regression tests

- Workspace/HEAD failures, tracked/staged/untracked/ignored/deleted changes,
  dirty baselines, rename ordering and unsafe paths: `snapshot_test.go`.
- Adapter nonignored baseline, ignored transitions and observed provenance:
  `internal/agent/codex/adapter_test.go`.
- Scope Gate rules, Git failure and warn/deny outcomes:
  `internal/gate/changed_files_test.go`.
- Baseline lifetime, correction attempts and cross-node workspace behavior:
  `internal/runtime/workspace_test.go`,
  `internal/runtime/changed_scope_integration_test.go`.

Run `go test ./internal/gitstate/...` before `make check`. For a snapshot
contract change, also run focused Codex, Gate and runtime suites. Update
this guide when an owner or contract changes.
