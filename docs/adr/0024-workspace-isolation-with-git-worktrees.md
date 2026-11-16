# ADR-0024 - Workspace isolation with Git worktrees and deterministic integration

**Status:** proposed (acceptance gated on one spike that needs a real Codex CLI, see "Validation")
**Date:** 2026-11-16
**Feature:** F3.1 (`docs/features/F3.1-workspace-isolation.md`)

## Context

F1.7 froze accepted runtime *values* but all Agents share one mutable checkout
(ADR-0015, ADR-0016). Consequences: a correction of unit C can overwrite files
produced by accepted unit B; unit-mode serializes same-checkout Agent cycles;
a rejected attempt has already written into the user's working tree; F1.8 only
detects out-of-scope changes after the fact. F3.1 must let parallel units edit
the same file without a filesystem race and keep unaccepted attempts out of the
result (roadmap acceptance criteria).

This decision changes workspace isolation, event semantics and which parts of
the user's repository the runtime writes to. All three are listed in
`AGENTS.md` as ADR triggers. The product owner accepted the recommended
defaults of the spec's open questions 1-9; this ADR records them as decisions.

Facts established by a spike on Git 2.43 (scratch repositories, not committed):

- `git worktree add` runs the `post-checkout` hook by default. With
  `-c core.hooksPath=/dev/null` and the controlled environment of
  `internal/gitstate` it does not.
- A repository-local filter driver (`filter.<name>.smudge`) **does run** during
  `git worktree add` even with a cleaned environment, no global/system config and
  hooks disabled. Overriding a *known* driver with `-c filter.<name>.smudge=cat`
  prevents it, but an unknown driver cannot be overridden.
- `git merge-tree --write-tree --merge-base=<base> <a> <b>` merges two commits
  into a tree object without a working tree or index. It exits 0 with the result
  tree on a clean merge and exits 1 with the tree plus the conflicted path names
  (`--name-only`) on conflict. It requires Git 2.38 or later.
- A tree can be committed and referenced from inside a linked worktree
  (`write-tree`, `commit-tree`, `update-ref`) without touching any branch, the
  source index or the source working tree.
- `gitstate.CanonicalWorkspace`, `Capture` and `Changed` work unchanged on a
  linked worktree root (where `.git` is a file) and report exactly the files
  changed inside that worktree.

## Decision

### 1. Mechanism: one linked Git worktree per isolated node run

An Agent node may set `config.isolation: worktree` (`none` default, absent means
`none`). The runtime creates a detached linked worktree at the run's base commit
before the node's first attempt and uses it as the effective workspace for the
Agent, its embedded Gate and the F1.8 baseline. The graph keeps naming only the
source repository; it never contains a worktree path.

The worktree lives for the whole node cycle, across correction attempts, so
F1.6 correction semantics ("correct what you wrote") are preserved. It is not
recreated per attempt.

`isolation: worktree` is valid only on a loop-enabled Agent that is enrolled in
`units`, because integration is triggered by the F1.7 accept-once transition.
Anything else fails preflight before `run_started`.

### 2. Base commit and dirty source

The base is the source HEAD resolved to a full SHA once at run start and
recorded in the trace. Preflight refuses a source with staged or unstaged
changes to tracked files (they would be silently absent from the worktree).
Untracked and ignored files are not carried over; this is a documented
limitation, not an error. No opt-out in v1.

### 3. Delivery: refs, never the user's tree

At acceptance the coordinator commits the worktree contents with the base SHA as
parent and stores it as `refs/agentplane/<run_id>/units/<ref-safe node>`.
Integration then runs in the coordinator, serially, in the existing
deterministic acceptance order (sorted node ID per batch), using
`git merge-tree --write-tree` of each unit commit onto the current integration
tip with the base as merge base. The tip is `refs/agentplane/<run_id>/integrated`,
initially the base SHA.

No integration worktree is needed. The runtime never modifies the source branch,
index or working tree. The only writes into the source repository are linked
worktree metadata under its common Git directory and refs under
`refs/agentplane/`. The user merges the integrated ref manually; an
`--apply-to-source` flag is explicitly rejected for v1 because it mutates the
user's tree and needs its own consequence analysis.

A conflict leaves the tip at the last clean commit, emits conflicting paths
(bounded, with an omitted count), retains that unit's worktree and ends the run
`needs_intervention` (existing CLI exit 4). Accepted values and siblings are not
touched or rerun. There is no automatic resolution, rebase or semantic merge.

### 4. Isolated nodes start from the run base only

Every isolated node starts from the base SHA. Downstream isolated nodes do not
see upstream file changes; data between nodes still flows only through declared
ports (consistent with AGENTS.md rule 4). Making upstream files visible
(`base = run base + accepted upstream units`) is deferred to a follow-up, F3.1b.

### 5. Scope boundaries

v1 isolates Agent nodes only. Standalone Shell, Gate and Human nodes keep the
source `cwd`/workspace and therefore do not observe integrated results. A Gate
embedded in an isolated worker's loop evaluates that worker's worktree.
Isolation is per node, not a graph-level default.

### 6. Scheduling

The effective workspace identity of an isolated node is its own worktree, so the
ADR-0015 rule "distinct linked worktrees remain concurrent despite a shared
common Git directory" applies without a new scheduler rule. An isolated node
does not conflict with a non-isolated Agent in the source checkout. Schema-7
declared-scope conflicts are not reported between isolated nodes.

### 7. Git hygiene (security-relevant)

All Git calls reuse the controlled invocation of `internal/gitstate`
(`/usr/bin/git`, structured argv, minimal environment, no system/global config,
`core.hooksPath=/dev/null`, `core.fsmonitor=false`, bounded time and output).
Hooks never run in runtime-created checkouts or snapshot commits.

Because an unknown repository-configured filter driver executes during
checkout and cannot be safely overridden, preflight **rejects** a source whose
local configuration defines any `filter.*.smudge`, `filter.*.clean` or
`filter.*.process`, with an actionable error naming the driver. It does not try
to blank drivers it can enumerate (that would silently change file content, for
example LFS pointers). Minimum Git version is 2.38, checked in preflight with a
clear error.

### 8. Storage root and lifecycle

Worktrees live under a runtime-owned root, mode `0700`, default
`os.UserCacheDir()/agentplane/worktrees`, overridable with
`--worktree-root PATH`. The root must be absolute and must not resolve inside
the source workspace (a worktree nested in the source would be observed by F1.8
snapshots and the source guard). The default is deliberately not
`.agentplane/runs`, which is cwd-relative and may sit inside the source repo.
Directory names derive from run ID and node-run ID, never raw node IDs; ref
names encode node IDs safely. Paths are confined and symlinks/pre-existing
targets refused. The runtime only ever removes paths it created in this run,
tracked by ID.

`--retain-worktrees never|failed|always` (default `failed`) decides removal.
Refs under `refs/agentplane/<run_id>/` are always kept: they are the deliverable.
Cleanup failure never changes run status; it is reported with the path.
Crash leftovers are not cleaned automatically; names carry an `agentplane-`
prefix so they are identifiable. Recovery and pruning belong to F3.3/F3.11. A
read-only `agentplane workspaces list` is allowed in v1 only if it is a small
addition; there is no `prune` in v1.

### 9. Source-workspace guard

A worktree is not a sandbox. When at least one isolated node ran, the runtime
takes a gitstate snapshot of the source at run start and compares it at run end.
A difference is an observed, unattributed `source_workspace_changed` fact with
bounded sorted paths. It never fails the run (the user may have edited the
checkout) and is never presented as enforcement. Confinement is F3.9.

### 10. Events

New typed, bounded facts, retained outside progress limits and deeply copied:
`workspace_prepared`, `workspace_integrated`, `workspace_conflict`,
`workspace_released`, `source_workspace_changed`. Order for an isolated accepted
unit: `gate_check` -> `return_envelope` -> `attempt_finished` ->
`workspace_integrated`. Operational failures emit no verdict and no envelope.
Payload contents are fixed by the feature spec's Observability section.

The event schema **number is not fixed here**. ADR-0023 (proposed) also claims
the next schema. The numbers are assigned by merge order at implementation:
the first to land takes 10, the other 11, and the later ADR is amended with the
final number. Both changes are additive; schemas 1-9 stay readable and consumers
that ignore unknown types keep working.

### 11. Code placement

Git worktree operations (create, snapshot commit, `merge-tree`, remove, ref
naming) go in a new package, `internal/worktree`, built on the shared
`gitstate` Git invocation. `internal/runtime` does not import it: it defines a
small consumer-owned interface and the CLI composes the concrete
implementation, as for Agent and Gate. The coordinator owns lifecycle state,
as it owns retry and acceptance state today. This section is guidance, not a
durable contract.

## Alternatives considered

### Alternative A: isolated checkout by `git clone --local` or file copy

Pros:

- No metadata written into the source's `.git`.
- Git metadata lives inside the sandbox-writable tree, which may matter for the
  Codex workspace-write sandbox (open risk below).

Cons:

- A clone has its own object database and refs; unit commits must be fetched
  back, adding a transfer step. A copy has no Git identity at all and F1.8
  baselines would need a synthetic repository.
- Larger disk cost on big repositories (unless hardlinked).
- Filters run on clone checkout as well, so it does not solve the filter risk.

Kept as the **documented fallback** if the Codex spike fails.

### Alternative B: shared checkout with stronger detection

Pros:

- No new lifecycle or storage.

Cons:

- Does not meet the roadmap criterion (parallel same-file edits, unaccepted
  attempt not reaching the main workspace). Detection after the fact cannot
  undo a write. This is the current F1.7/F1.8 state.

### Alternative C: apply accepted units directly to the user's branch/working tree

Pros:

- The user sees results immediately; no manual merge.

Cons:

- Mutates the user's tree, conflicts with their uncommitted edits, hard to
  reverse, and a conflict mid-run leaves a half-applied tree. Rejected for v1.

### Alternative D: integrate in a dedicated integration worktree with `git merge`

Pros:

- Familiar mechanism; a downstream Shell/Gate could run in the integrated tree.

Cons:

- Needs a second checkout and working-tree merge state to clean up after
  conflicts. `merge-tree --write-tree` gives the same result without any working
  tree. Revisit with F3.1b if downstream nodes must run against the integrated
  tree.

### Alternative E: blank known filter drivers instead of rejecting repositories

Pros:

- Works on LFS repositories.

Cons:

- Unknown drivers cannot be enumerated reliably across config includes, and
  blanking silently changes checked-out content. Rejection is conservative and
  actionable; it can be relaxed later by an explicit decision.

## Consequences

### Positive

- Real parallelism for isolated units and no file-level interference between
  them. The ADR-0015 consequence "shared files may change under accepted
  siblings" no longer applies to isolated units.
- Unaccepted attempts never leave their worktree; the user's tree is untouched.
- Deterministic, conflict-explicit integration with auditable commits.
- Reuses existing Git helpers, scheduler rules and F1.8 baselines.

### Negative / trade-offs

- Worktree metadata and `refs/agentplane/*` appear in the user's repository.
- Fresh worktrees lack untracked/ignored build inputs such as `node_modules`.
- Repositories with filter drivers (notably LFS) and Git older than 2.38 are not
  supported in v1.
- No downstream file visibility; pipelines that need it wait for F3.1b.
- Standalone Shell/Gate nodes cannot yet verify the integrated result.
- Disk usage grows with concurrent isolated nodes; no quota.
- Crash leftovers need manual cleanup until F3.3/F3.11.
- Still not a sandbox: absolute-path writes escape. The source guard only
  detects.
- Linux and macOS only.

## Compatibility and migration

Existing graphs are unchanged: `isolation` is optional, schema_version stays 0,
and the semantic hash includes the field only when present. Existing event
streams, the run store (JSONL stores `event_schema_version` per event, ADR-0019)
and the desktop reducer are unaffected; new types are additive. CLI envelope
version stays 1 with an optional `workspace_isolation` object. F2.9 and the
loopback API need no change.

## Validation

Deterministic, temporary Git repositories with a fake adapter and explicit
barriers (no sleeps): parallel same-file edits; scoped retry writing to a
sibling's path with real files; deterministic integration over 20 runs;
conflict handling and exit 4; hostile node IDs in paths and refs; hooks and
filter-driver rejection (a canary file proves neither ran); dirty-source,
missing-HEAD, old-Git and unsafe-root preflight; retention table; cancellation;
source guard; schema round-trip with old fixtures; `go test -race ./...` and
`make check`.

**Acceptance gate for this ADR (not yet done):** a short authorized spike with a
real installed Codex CLI in a disposable repository, recorded under
`docs/validation/`, must show that Codex's workspace-write sandbox can run in a
linked worktree whose gitdir lives in the source's `.git`, and in particular
that its own `git status`/`git diff` calls do not fail. If it fails, switch the
mechanism to Alternative A (same lifecycle and integration contract) and amend
this ADR before implementation. The status stays `proposed` until then.
