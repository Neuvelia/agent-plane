# ADR-0024 - Attempt diff capture for Attempt Compare

**Status:** proposed (team decisions D2-D6 open; see "Decisions for the team")
**Date:** 2026-11-15

## Context

F2.8 Attempt Compare must show whether a correction attempt fixed what the Gate rejected. The engine on
`main` (event schema 9) records per attempt: `attempt_started`, `attempt_input` (feedback linkage, no raw
task), `gate_check` (CheckResult v1, ADR-0013), `attempt_finished`, and for Agent producers a
`node_evidence` with `attempt_id` listing Git-observed changed **paths** for that one invocation
(ADR-0010, ADR-0014). The feedback a retried producer receives is a clone of the previous `gate_check`
payload, so it is already fully reconstructible. A `changed_files_scope` Gate (ADR-0016) adds cumulative
out-of-scope paths.

No file **contents** are recorded. `internal/gitstate` holds index entries and SHA-256 digests of working
files in memory only, and discards them. A real attempt diff therefore needs a new capture contract.

Constraints:

- Local-first; no remote service; store under the run directory with 0700/0600 (ADR-0019).
- Trace is correctness (AGENTS.md rule 8): the UI derives state by reducing events (ADR-0019, F2.5).
- No secrets in events; redaction is explicit-list only (`runtime.Options.LogRedactions`), and `serve`
  has no `--redact-env` yet (ADR-0019, ADR-0023).
- Execution code treats agent output and repository content as untrusted (rule 10). Git inspection must not
  run hooks, filters or monitors (ADR-0010).
- Edits persist across attempts; there is no isolation until F3.1 (ADR-0014, ADR-0015). Concurrent writers
  in the shared workspace can be attributed to the attempt (ADR-0016).
- `gitstate` calls `/usr/bin/git` with `/dev/null` config paths: it works on Linux, macOS (Command Line
  Tools shim) and WSL, not on a native Windows engine.

## Options

### Option A - Engine-rendered bounded patch per attempt

At the start of the first attempt and after each attempt's producer finishes, take a content-aware
snapshot of the Git workspace; render the difference between consecutive snapshots as a unified patch per
changed file; redact, bound and emit it as events.

Snapshot mechanism (recommended within A): reuse the existing `gitstate` pass, which already reads every
non-ignored working file to hash it. For before-content, a tracked file whose working bytes hash (as a Git
blob, computed in Go during the same read) to its index blob id is recoverable from the repository object
database (read with `git cat-file --batch`, which applies no filters). Any other file, including tracked
files that differ only through eol conversion or clean/smudge filters, counts as dirty: a dirty or
untracked non-ignored text file up to 1 MiB is copied into a private
per-run directory under the store (0700), keyed by its SHA-256, and deleted when the run ends. Nothing is
written into the user's `.git`. Ignored files are listed by path, never read. Patches are rendered in Go
(Myers line diff, small and dependency-free) so no Git diff machinery, textconv or external driver can run.

Rejected sub-variant A0: `git diff HEAD` at attempt end. It is cheap but cumulative since HEAD, includes
pre-existing dirt and earlier attempts' edits, omits untracked files, and runs configured diff drivers
unless carefully disabled. It cannot attribute changes to an attempt.

Rejected sub-variant A1: `git add -A` into a temporary index plus `write-tree`. It runs clean filters
(for example LFS) from `.gitattributes`, and writes objects into the user's repository unless an alternate
object directory is configured.

- **Privacy / secrets:** stores only the bounded, redacted patch; raw before-copies exist only during the
  run in a private directory. Unknown secrets in changed lines are recorded (explicit-list redaction).
  Ignored files (usual home of `.env`) are never read.
- **Size bounds:** 16 KiB per event, 256 KiB per attempt, 4 MiB per run; files over 1 MiB or binary get
  hashes and sizes only. Temporary copies are bounded by the same 1 MiB per-file limit.
- **Local storage:** inside `events.jsonl` (D4 inline) plus a temporary private directory.
- **Windows / macOS / Linux:** inherits `gitstate`; works on Linux, macOS and WSL. Native Windows needs Git
  discovery (D5). Line endings: patches compare bytes as stored on disk; `\r\n` changes appear as changes.
- **Cost:** one extra read of dirty/untracked files at each snapshot (already read for hashing), small
  copies, Go diff over changed files only. Clean tracked files cost one `cat-file` read when they change.
- **Event schema:** one new event type `attempt_diff`, additive schema bump.
- **What the UI can show:** per-attempt per-file patches, line counts, hashes, change kinds, omissions.
  For consecutive attempts the later patch is the change from the earlier result; for non-consecutive
  attempts the UI lists intermediate patches rather than composing them.

### Option B - Content-addressed blobs of changed files

At the same snapshot points, store full before/after contents of each changed file as SHA-256-addressed
blobs under the store (`<store>/blobs/` or per run), referenced from events; the UI (or an API route)
computes diffs on demand.

- **Privacy / secrets:** stores full raw file contents, unredacted unless redaction is applied at write,
  which defeats exact content addressing; exposure is much larger than A. Any secret in a changed file is
  persisted whole.
- **Size bounds:** per-file cap still needed; total grows with file size, not change size. Dedupe helps
  across attempts and runs only with a shared store, which needs garbage collection and retention (F3.2).
- **Local storage:** a second storage format next to `events.jsonl`; a new authenticated route to read
  blobs; replay no longer comes from the event stream alone.
- **Windows / macOS / Linux:** same Git dependency as A; plus file path and permission handling for the
  blob store on each platform.
- **Cost:** highest disk use; simplest capture code; diff work moves to the reader.
- **Event schema:** new event type with blob references; new API route and its tests.
- **What the UI can show:** the richest view: full files, any pair of attempts, arbitrary context, word
  diff, and a true "To vs From" diff regardless of truncation.

### Option C - Compare only what is already recorded

No new capture. Compare outcomes, verdicts, reasons, evidence, durations, feedback linkage, per-attempt
observed paths (Agent producers) and `changed_files_scope` evidence.

- **Privacy / secrets:** no new exposure.
- **Size bounds:** none new.
- **Local storage:** unchanged.
- **Windows / macOS / Linux:** unchanged (paths are already Git-observed where `gitstate` works).
- **Cost:** desktop-only work.
- **Event schema:** unchanged; works for every stored run from schema 6.
- **What the UI can show:** side-by-side facts, "new in attempt N" path badges, scope badges, feedback. No
  content diff; Shell producers show "not observed" for files.

## Decision (recommended, pending team acceptance)

1. **Ship Option C now as F2.8a** (no contract change), independent of the rest of this ADR.
2. **Adopt Option A for F2.8b**, with the snapshot mechanism described under A: Go-rendered patches,
   before-content from the object database for clean tracked files and from private temporary copies for
   dirty/untracked files, no writes to the user's repository, ignored files by path only.
3. **Opt-in, off by default:** `--record-diffs` on `agentplane run` and `agentplane serve`; a separate
   desktop setting "Record file changes for attempt compare". The desktop may enable it only after `serve`
   supports `--redact-env`.
4. **Inline events:** `attempt_diff` per changed file per captured attempt plus one summary event (empty
   `path`) with totals, omissions and any capture diagnostic; emitted after the producer's `node_evidence`
   and before `attempt_finished`. Payload: `path`, `change` (`added|modified|deleted|mode`), `binary`,
   `before_sha256`, `after_sha256`, `before_size`, `after_size`, `added_lines`, `deleted_lines`, `patch`,
   `truncated`, `redacted`, `omitted_reason` (`binary|too_large|ignored|budget|unreadable`), and on the
   summary `files`, `omitted_files`, `diagnostic_code`. Renames are an old-path deletion plus a new-path
   addition, matching ADR-0016.
5. **Snapshot points:** before attempt 1 and after each producer finishes, before its Gate. Attempt N's
   diff is snapshot(N-1 end or initial) to snapshot(N end). Only loop producers with a Git workspace
   accepted by `gitstate.CanonicalWorkspace` (Agent today) are captured.
6. **Capture never decides execution:** a failure is recorded in the summary event and leaves outcome,
   verdict, retry and acceptance unchanged. This differs from deny-mode `changed_files_scope`, where
   inspection failure is an operational error, because capture is evidence for people, not a check.
7. **Git safety:** reuse `gitstate`'s controlled environment; never run hooks, clean/smudge filters,
   textconv, external diff drivers or fsmonitor; read objects with `cat-file --batch` only.

## Decisions for the team

- **D2 - Option:** A (recommended), B, or C only.
- **D3 - Default and switch:** opt-in (recommended) or default-on; separate switch (recommended) or shared
  with ADR-0023's value recording.
- **D4 - Storage:** inline events (recommended) or side files plus a new authenticated route
  (`GET /runs/{id}/attempts/{attempt}/diff`). Side files keep events small and allow deletion, at the cost
  of a second read path and replay consistency.
- **D5 - Native Windows:** accept "not available" (recommended) or add Git discovery to `gitstate` first
  (separate ADR; also changes ADR-0010/0016 behaviour on Windows).
- **D6 - Shell producers:** Agent only (recommended) or also Shell producers whose `cwd` is a Git root.
- **Budgets:** confirm 16 KiB / 256 KiB / 4 MiB and the 1 MiB per-file limit.

## Consequences

### Positive

- F2.8 delivers useful comparison immediately with no new exposure (Option C).
- With A enabled, the UI can show what each attempt changed, live and after restart, from one read path.
- Exposure is explicit, bounded, redacted and off unless enabled; the user's repository is never written.
- Capture inherits the proven `gitstate` safety rules rather than introducing a second Git integration.

### Negative / trade-offs

- With A enabled, `events.jsonl` holds repository content; unknown secrets in changed lines are recorded;
  there is no retention or deletion tool until F3.2.
- Truncated patches and per-attempt patches (no direct diff between non-consecutive attempts) limit the
  view; B would not have those limits.
- Shared workspace: concurrent writers (another Shell node, the user's editor) can appear in an attempt's
  diff, exactly as they can appear in ADR-0016 evidence. The UI must say diffs are workspace observations.
- Snapshot cost grows with the number of dirty/untracked files; very large untracked trees that are not
  ignored make snapshots slow. Existing `gitstate` limits (16 MiB listing, 5 s per Git call) apply.
- A Go line-diff implementation is new code to maintain (no new dependency).
- Native Windows engines show "not available" until Git discovery exists.

## Compatibility and migration

- Option C: no change to events, store, API, graph schema or CLI envelope.
- Option A: one additive event type and an event schema bump (10 if it lands before ADR-0023's
  `value_recorded`, otherwise 11). Readers of schema 1-9 streams are unchanged; older readers reject the new
  schema as they do for any new contract. Default CLI and serve streams are unchanged without
  `--record-diffs`. Stored runs are never rewritten. A later SQLite or F3.2 store ingests the events as-is.
- Option B would additionally introduce a blob store layout and API route that F3.2 must migrate.

## Validation

- F2.8a: desktop reducer and compare model tests on recorded fixtures; live versus replay equality; e2e
  from the node panel.
- F2.8b: temporary Git repositories for tracked, staged, untracked, ignored, deleted, renamed and
  mode-only changes, pre-existing dirt and two attempts editing the same file; attribution per attempt (not
  cumulative); redaction across the cut; budgets and omitted counts; markers proving no hook, filter,
  textconv or diff driver ran and no object was written into `.git`; Git failure leaving loop semantics
  unchanged; no `attempt_diff` without the flag; old schemas decode. `go test -race ./...`, `make check`,
  `make desktop-check`, `make desktop-e2e`.
