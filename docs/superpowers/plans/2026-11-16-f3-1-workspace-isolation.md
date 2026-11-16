# F3.1 Workspace Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an enrolled loop Agent node run in its own Git worktree from an
explicit base commit, integrate only accepted units deterministically onto a
run-scoped ref, and leave the user's checkout untouched.

**Architecture:** A new `internal/worktree` package owns Git worktree, snapshot,
`merge-tree` and ref operations on top of one hardened Git runner shared with
`internal/gitstate`. `internal/runtime` stays free of concrete imports: it owns
lifecycle and acceptance state through a small consumer-owned interface, and
`cmd/agentplane` composes the implementation. Five typed events carry the trace.

**Tech Stack:** Go (standard library only, no new dependency), `/usr/bin/git`
2.38+, existing event/runtime/CLI packages.

**Spec:** `docs/features/F3.1-workspace-isolation.md`.
**ADR:** `docs/adr/0024-workspace-isolation-with-git-worktrees.md` (accepted).
**Evidence:** `docs/validation/F3.1-codex-worktree-spike.md` (PASS).

## Global Constraints

- Implement only F3.1 as specified. No downstream file visibility (F3.1b), no
  Shell/Gate/Human isolation, no `--apply-to-source`, no pruning, no resume.
- Graphs and runs without `isolation` must keep byte-identical behavior: same
  semantic hashes, event sequences and scheduling. Existing tests pass
  unmodified.
- The runtime never writes the user's branch, index or working tree. The only
  writes into the source repository are linked-worktree metadata and
  `refs/agentplane/*`.
- All Git goes through the hardened runner: `/usr/bin/git`, structured argv,
  minimal environment, no system/global config, hooks and fsmonitor off, bounded
  time and output. Raw Git stderr never reaches events or CLI output.
- No sleeps as synchronization in tests; use barriers and channels. Temporary
  Git repositories only.
- Keep `internal/runtime` free of imports of `internal/worktree` (production).
- One branch and one PR per task below, in order. Never merge without an
  explicit request. Each task ends with `make check` and
  `go test -race ./...` and a review of `git diff --stat`.
- Keep a local `TASKS.md` journal (Git-ignored) while implementing, per
  `AGENTS.md`.

## Review Focus

- Path and ref-name safety (hostile node IDs, symlinks, pre-existing targets).
- Hooks and filter drivers cannot execute; the canary tests really prove it.
- Event ordering (`workspace_integrated` after `attempt_finished`) and schema
  compatibility (old fixtures still decode).
- Coordinator-only mutation of lifecycle state; no goroutine touches
  integration state.
- That a conflict, a snapshot failure or a cleanup failure never rewrites
  accepted values or reruns a sibling.
- Cleanup removes only paths this run created.

## Task order and dependencies

```text
T1 git runner + internal/worktree ──┐
T2 events (schema, payloads)        ├─> T3 runtime ──> T4 CLI/API ──> T5 guard + demo
                                    ┘
```

T1 and T2 are independent and can be built in parallel by different people.

---

### Task 1: Shared Git runner and `internal/worktree`

**Branch:** `claude/f3-1-worktree-package` (implemented; PR open)

**Files:** Create `internal/worktree/{doc,worktree,integrate,refs,preflight}.go`
and `*_test.go`, `internal/worktree/AGENTS.md`. Edit
`internal/gitstate/snapshot.go` (extract runner) and
`internal/gitstate/AGENTS.md`. Edit `docs/codebase-map.md`.

**Prerequisite seam (`internal/gitstate`):** `git()` today has a fixed 5-second
timeout, discards stderr, and its error text is specific to inspection. That is
too short for `worktree add` on a large repository and too narrow for commit
creation. Extract one exported hardened runner, for example
`gitstate.Run(ctx, dir, RunOptions{Timeout, StdoutLimit}, args...)`, with the
same fixed environment and the `-c core.fsmonitor=false -c
core.hooksPath=/dev/null` prefix; make `git()` call it with its current
limits so behavior is unchanged. Callers pass `-c user.name=... -c
user.email=...` as leading arguments when they need a commit identity.

**Interface (`internal/worktree`), indicative:**

```go
type Source struct{ Root, BaseSHA string }          // canonical, resolved once
func Preflight(ctx, sourceWorkspace, worktreeRoot string) (Source, error)
type Manager struct{ /* root dir, 0700 */ }
func NewManager(root string) (*Manager, error)
func (m *Manager) Create(ctx, src Source, runID, nodeRunID string) (*Handle, error)
func (h *Handle) Path() string
func (h *Handle) Snapshot(ctx, ref string) (commit string, err error)
func (m *Manager) Integrate(ctx, src Source, tipRef, unitCommit string) (IntegrateResult, error)
func (m *Manager) Remove(ctx, h *Handle) error
func UnitRef(runID, nodeID string) (string, error)
func IntegratedRef(runID string) string
```

Rules fixed by the ADR that the package enforces:

- `Preflight`: absolute source resolving to a non-bare worktree root with
  committed HEAD (reuse `gitstate.CanonicalWorkspace`); no submodule gitlinks;
  no staged/unstaged tracked changes (`status --porcelain=v1
  --untracked-files=no`); Git >= 2.38; **reject** any `filter.*.smudge|clean|
  process` visible through repository config including included files; worktree
  root absolute, not resolving inside the source, created 0700 without
  following symlinks. Errors are classified and actionable (name the driver or
  the dirty condition), never raw Git output.
- `Create`: `worktree add --detach` at the resolved SHA; directory name from
  validated run and node-run IDs only; refuse an existing or symlinked target.
- `Snapshot`: `add -A`, `write-tree`, `commit-tree -p <base>` with a fixed
  identity, `update-ref`; never touches a branch.
- `Integrate`: `merge-tree --write-tree --name-only --no-messages
  --merge-base=<base> <tip> <unit>`; exit 0 -> new tip commit and `update-ref`;
  exit 1 -> conflicting paths (bounded to 64 plus an omitted count), tip
  unchanged; other exit -> operational error.
- `UnitRef`: injective and short. Git limits a loose-ref path component to the
  filesystem's 255 bytes and enrolled node IDs may reach 256 bytes, so use a
  sanitized readable prefix (<= 40 bytes of `[A-Za-z0-9_-]`) plus a
  `sha256[:16]` of the raw ID. Never `..`, a trailing `.lock`, or a control
  character.
- `Remove`: removes only handles created by this manager instance (tracked by
  ID, not by a path argument); never the source.

- [x] Write failing tests first (below), then extract the runner with `git()`
  behavior unchanged (existing `internal/gitstate` and Codex tests pass untouched).
- [x] Implement the package to the interface above.
- [x] Add `AGENTS.md` for the package and update the codebase map.

**Tests (temporary Git repos, no Codex):**
`TestPreflightRejectsDirtyTrackedAndAcceptsUntracked`,
`TestPreflightRejectsFilterDriverIncludingIncludedConfig` (canary file proves
the driver never ran), `TestPreflightRejectsOldGit` (injected version),
`TestPreflightRejectsSubmoduleAndBareAndMissingHead`,
`TestPreflightRejectsRootInsideSource`, `TestCreateDoesNotRunPostCheckoutHook`
(canary), `TestCreateRefusesSymlinkedAndExistingTarget`,
`TestRefNamesHostileNodeIDs` (`../x`, spaces, `x.lock`, non-ASCII, 256-byte IDs,
two IDs that sanitize identically stay distinct), `TestSnapshotDoesNotMoveBranchIndexOrWorkingTree`,
`TestIntegrateDisjointCleanAndDeterministic` (20 repetitions, same commit tree),
`TestIntegrateConflictReportsPathsAndKeepsTip`, `TestIntegrateConflictBounds`,
`TestRemoveOnlyOwnHandle`, `TestGitstateWorksOnLinkedWorktree` (Capture/Changed on
a linked worktree, where `.git` is a file), runner tests for timeout, output
limit and stderr never returned.

---

### Task 2: Events

**Branch:** `claude/f3-1-events`

**Files:** Create `internal/events/workspace.go`, `workspace_test.go`. Edit
`internal/events/event.go` (schema constant, `Validate` switch, clone),
`internal/events/AGENTS.md`, `docs/architecture.md` (event list).

**Contract (from the spec's Observability section):**

| Type | Payload (all strings bounded, slices detached on clone) |
| --- | --- |
| `workspace_prepared` | `base_sha`, `mode` (`worktree`), `path`, `source_root` |
| `workspace_integrated` | `unit_ref`, `unit_commit`, `integrated_ref`, `integrated_commit`, `changed_paths` (count) |
| `workspace_conflict` | `unit_ref`, `unit_commit`, `tip_commit`, `paths` (<= 64), `omitted` |
| `workspace_released` | `action` (`removed` / `retained`), `reason`, `path`, `cleanup_error` (class only) |
| `source_workspace_changed` | `paths` (sorted, bounded), `omitted`, `indeterminate` (T5) |

- [ ] **Schema number by merge order.** Check `origin/main` for the current
  `events.SchemaVersion` and for ADR-0023's `value_recorded` work. Take the next
  free number, gate the new types on it in `Validate`, and amend the ADR text
  that says "assigned by merge order" with the final number in the same PR.
- [ ] Strict validation: correlation fields (run, node, node run) required where
  the event is node-scoped; reject payloads whose size/count exceed bounds;
  reject new types below the new schema.
- [ ] Round-trip and old-fixture tests: schemas 1-9 fixtures still decode; the
  JSONL store (`internal/store`) writes and reads the new types unchanged.
- [ ] Verify the desktop reducer ignores unknown event types (read
  `desktop/src/reducer.ts`); add a reducer test with a `workspace_*` event so
  the tolerance is a tested contract, not an assumption.

**Tests:** `TestWorkspaceEventsRoundTripAndBounds`,
`TestWorkspaceEventsRequireNewSchema`, `TestWorkspaceEventsCloneIsDetached`,
`TestOlderSchemaFixturesStillDecode`, store round-trip test, desktop reducer test.

---

### Task 3: Runtime integration

**Branch:** `claude/f3-1-runtime`  (depends on T1 and T2)

This is the largest task. Land it as ordered commits, each green, in this order.

**Files:** Edit `internal/agent/config.go` (accept `isolation`),
`internal/runtime/{scheduler,preflight,batch,accept,workspace,result,loop}.go`,
create `internal/runtime/isolation.go` and tests, edit
`internal/runtime/AGENTS.md`, `docs/graph-spec.md`, `docs/architecture.md`.

**Seams (verified in the current code):**

- Agent config is strict: `internal/agent/config.go` rejects unknown fields, so
  add `isolation` to the accepted set and validate `none|worktree`. The
  semantic hash already includes node `config` content, so no hash code
  changes; add the hash-stability test.
- The runtime-owned interface, in `internal/runtime/isolation.go`, with
  `Options.Workspaces` (nil means any `isolation: worktree` fails preflight with
  an actionable message) and `Options.RetainWorktrees` (`never|failed|always`,
  zero value = `failed`).
- Effective workspace: `cycle()` and the embedded Gate read
  `n.Config["workspace"]`. At dispatch, build a deep-copied node whose
  `config.workspace` and loop Gate `workspace` equal the worktree path, and pass
  that to `cycle`. The graph definition and the semantic hash stay untouched.

**Commits (in order):**

- [ ] **3a Preflight.** `preflightIsolation`: `isolation` valid only on a
  loop-enabled Agent enrolled in `units`; otherwise a `PreflightError` naming
  the node, before `run_started`. Call `Workspaces.Preflight` once per distinct
  source; keep `Source` (base SHA) in `runPreparation`. Nothing emitted yet.
- [ ] **3b Scheduling identity.** In `workspace.go`, give an isolated node a
  synthetic known identity with a unique root outside any source (distinct
  worktree and gitDir), so `workspaceConflict` is false against every other
  node. Skip isolated nodes in `reportScopeConflicts`. Non-isolated behavior is
  unchanged.
- [ ] **3c Lifecycle.** In `runBatchWorkers`, on a node's first attempt, the
  coordinator creates the worktree (serial, ctx-aware), emits
  `workspace_prepared`, and records the handle in coordinator-owned state next
  to `batchState`. Creation is on the coordinator on purpose: deterministic
  event order and no goroutine touches lifecycle state. Revisit only if
  profiling shows batch start latency matters. Failure is an operational error
  (`FailureExecutor`, safe message, no verdict, no envelope); siblings continue.
- [ ] **3d Integration.** In `acceptBatchResults`, after `acceptedValues`
  succeeds and before `transitionNode(NodeSucceeded)`: `Snapshot` ->
  `Integrate` -> `workspace_integrated`, strictly in the existing sorted-node
  order. Snapshot or integrate operational failure: node fails (`FailureExecutor`),
  accepted values already frozen stay recorded, no rerun.
- [ ] **3e Conflict.** On conflict: emit `workspace_conflict`, keep the tip,
  retain that worktree, and fail the node with `FailureNeedsIntervention` so the
  run ends `needs_intervention` (CLI exit 4) and dependents are skipped, exactly
  as budget exhaustion does today. Recorded `Outputs` and `AcceptedAttemptID`
  stay on the node result. (See "Decisions to confirm" 1.)
- [ ] **3f Release.** Apply retention at node terminal states and a sweep in
  `finish()` for any handle left, emitting `workspace_released` for each.
  `never` removes all; `failed` removes accepted-and-integrated units only;
  `always` removes none. Removal failure never changes run status.
  Cancellation: workers have returned before coordinator cleanup, so processes
  are already ended; assert that order in a test.
- [ ] **3g Result.** Add `RunResult.Isolation` (base SHA, integrated ref and
  commit, per-unit refs/commits, retained paths) populated from coordinator
  state only.

**Tests (runtime_test with a fake `Workspaces` plus a real-Git integration set):**

Use or extend `internal/agent/adaptertest` with a scripted adapter that can edit
files under `input.Workspace` and block on a barrier; the existing mock only
returns canned output.

- `TestIsolationParallelSameFileHasNoRace` - two units rewrite `shared.txt`,
  both reach a barrier before either returns, each worktree holds only its own
  content, and the source is byte-identical afterward (spec criteria 1 and 2).
- `TestIsolationScopedRetryProtectsAcceptedSiblingFiles` - A/B/D accepted, C red
  then green, C's retry writes B's path; B's unit commit and the integrated tree
  hold B's content; invocation counts 1/1/1 with C at 2 (criterion 5, closes the
  ADR-0015 limitation).
- `TestIsolationWorktreePersistsAcrossAttempts` and
  `TestIsolationExhaustedUnitProducesNoRef`.
- `TestIsolationIntegrationDeterministic` (20 runs) and
  `TestIsolationConflictNeedsInterventionKeepsAcceptedValues`.
- `TestIsolationPreflightFailsBeforeRunStarted` (non-enrolled, non-loop, dirty
  source, invalid value, nil manager; assert no `run_started`).
- `TestIsolationAbsentPreservesHashEventsAndScheduling` (existing unit tests
  plus a hash fixture).
- `TestIsolationEventOrder` (`attempt_finished` before `workspace_integrated`),
  `TestIsolationRetentionTable`, `TestIsolationCancellationReleasesAfterProcessEnds`,
  `TestIsolationSnapshotFailureDoesNotRewriteAcceptedValues`,
  `TestIsolationCleanupFailureDoesNotChangeStatus`.
- F1.8 `changed_files_scope` deny mode inside a worktree flags only that
  worktree's out-of-scope file (criterion).
- Run `go test -race ./internal/runtime/...` repeatedly (`-count=20`) on the
  concurrency tests.

---

### Task 4: CLI, API composition and result envelope

**Branch:** `claude/f3-1-cli`  (depends on T3)

**Files:** Edit `cmd/agentplane/{cli,serve,json,output}.go`, tests,
`cmd/agentplane/AGENTS.md`, `docs/features/F0.7-cli-runner.md` (flags only if
that spec lists flags), `docs/graph-spec.md`.

- [ ] Flags on `run` and `serve`: `--worktree-root PATH` (absolute; default
  `os.UserCacheDir()/agentplane/worktrees`, never the cwd-relative
  `.agentplane/runs`) and `--retain-worktrees never|failed|always`. Reject
  invalid values and relative roots with usage errors; update the usage string
  at `cli.go:57`.
- [ ] Compose `worktree.Manager` into `runtime.Options` only when needed; no
  Git or directory work for graphs without isolation.
- [ ] JSON result (`--json`): optional `workspace_isolation` object, absent when
  unused; envelope version stays 1. Human output prints the base SHA, the
  integrated ref and commit, and retained paths with a one-line "a worktree is
  not a sandbox" note and the manual merge command. Cleanup warning prints the
  path to remove.
- [ ] `serve`: same options via `POST /runs`; no new endpoint. Document that API
  runs use the defaults.
- [ ] Optional, only if tiny: `agentplane workspaces list` (read-only, lists
  `agentplane-*` entries under the root). Otherwise leave to F3.11.

**Tests:** built-binary CLI test with a fake installed Codex that edits files in
`--cd` (exit 0 with refs in JSON; exit 4 on conflict; preflight failure exit
code and message; flag validation); `serve_test` for an isolated run via the API;
golden JSON for the absent-isolation case (unchanged bytes).

---

### Task 5: Source guard, validation recipe and closure

**Branch:** `claude/f3-1-guard-and-docs`  (depends on T4)

**Files:** Edit `internal/runtime/{preflight,isolation,scheduler}.go`, create
`docs/validation/F3.1-real-codex-demo.md`, edit `docs/features/F3.1-*.md`,
`docs/features/README.md`, `docs/roadmap.md`, `docs/adr/README.md`,
`docs/architecture.md`, `docs/graph-spec.md`, `AGENTS.md` (phase paragraph),
`docs/codebase-map.md`.

- [ ] **Source guard.** When at least one isolated node ran, take a
  `gitstate.Capture` of the source before `run_started` (in preflight) and compare
  at run end; emit `source_workspace_changed` with sorted bounded paths. A guard
  failure is non-fatal. **Spec amendment:** `Capture` hashes ignored files, which
  can fail or be slow on large ignored trees (documented in ADR-0016), so an
  inspection failure emits the same event with `indeterminate: true` instead of
  silence. Never fails the run, never attributed.
- [ ] Tests: `TestSourceGuardReportsAbsolutePathWrite` (fake agent writes to the
  source path), `TestSourceGuardSilentWhenUnchanged`,
  `TestSourceGuardIndeterminateDoesNotFailRun`.
- [ ] **Real-Codex demo recipe**, modeled on `F1.7-real-codex-demo.md`: the
  A/B/C/D scoped-retry graph with `isolation: worktree`, proving bad -> red ->
  correct within three attempts, only the failed unit retried, and the sibling's
  file intact in the integrated ref. Prepared, not executed.
- [ ] **Docs closure:** F3.1 spec status to implemented (honest about what is
  not: no downstream visibility, no Shell isolation, no prune); update the
  roadmap Phase 3 note, the `AGENTS.md` "Current product phase" paragraph, graph
  spec `isolation` section, architecture workspace-isolation section (replace
  "Workspace isolation direction" progression with what exists), and mark F3.1
  limitations that remain (Agent task text must not stage or commit; not a
  sandbox; Linux/macOS only).
- [ ] Final review pass: run the `code-review` skill on the combined diff, then
  `make check`, `go test -race ./...`, and the spike script once more against the
  built result if Codex is available.

---

## Decisions to confirm before T3

1. **Node state on conflict.** The plan fails the node with
   `FailureNeedsIntervention`, which skips dependents but keeps the recorded
   accepted outputs. The spec only says the run ends `needs_intervention`.
   Alternative: keep the node `succeeded` and mark only the run. The plan's
   choice matches how exhaustion behaves and avoids downstream work on an
   incomplete integration.
2. **Coordinator creates worktrees (serially).** Chosen for deterministic events
   and single-owner state. If large repositories make batch start too slow, move
   creation into workers (like baselines) with the event emitted at acceptance.
3. **Failure kind for operational worktree errors.** Uses the existing
   `FailureExecutor` to avoid new vocabulary in `run_finished` and the CLI. A
   dedicated `workspace` kind is possible later.
4. **Guard emits when indeterminate** (spec amendment above).
5. **Optional `workspaces list`** in T4: yes only if it is a few dozen lines.

## Risks and edge cases

- **Hooks and filters** are the largest security surface. Canary tests must
  prove non-execution, and the filter check must see configuration reached
  through `include`/`includeIf`.
- **Large repositories:** a full checkout per isolated node, and `Capture` in the
  guard. Concurrency is bounded by `--max-concurrency`; no quota in v1.
- **Disk and orphans:** a crash leaves worktrees. Names are `agentplane-`
  prefixed; cleanup is F3.3/F3.11.
- **Apple Git vs Git 2.43:** the spike and the ADR evidence used different
  versions; `merge-tree --write-tree` behavior matches in both, but T1 runs on
  whatever Git CI has, so pin a minimum in CI logs.
- **macOS temp dirs:** `/var` -> `/private/var` symlink differences. Canonicalize
  with `EvalSymlinks` before every containment and equality check; test both.
- **Event schema collision** with ADR-0023 is resolved at T2 by merge order.
- **Codex sandbox:** the agent cannot stage or commit in its worktree (spike
  result). Task text and any prompt templates must not ask for it.

## Definition of done for F3.1

Every acceptance criterion in the spec is covered by a named test above;
`make check` and `go test -race ./...` pass in CI; docs and indexes are
consistent; the real-Codex demo recipe exists (execution may follow separately,
as with F1.7).
