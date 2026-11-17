# F2.8b Attempt Diff Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** With `--record-diffs`, record what each Agent correction attempt
changed in the Git workspace as bounded, redacted `attempt_diff` events, so
Attempt Compare can show per-attempt patches. Off by default; a run without the
flag is byte-identical to today.

**Architecture:** A pure line-diff package (`internal/domain/diff`) renders
bounded unified patches. A Git adapter (`internal/adapter/attemptdiff`) takes
content-aware workspace snapshots through the hardened `gitstate.Run` and
compares two of them. `internal/runtime` owns the policy (when to snapshot, the
run budget, redaction, event order) behind a consumer-owned port; the CLI and
`serve` compose the adapter. One new event type, `attempt_diff`, event schema 12.

**Tech Stack:** Go (standard library only, no new dependency), `/usr/bin/git`
(`ls-files`, `cat-file blob` only), existing event/runtime/CLI packages.

**Spec:** `docs/features/F2.8-attempt-compare.md` (F2.8b, FR10-FR18).
**ADR:** `docs/adr/0026-attempt-diff-capture.md` (proposed; owner decisions
D1-D6 recorded 2026-11-15; moves to accepted in the last task).
**Issue:** Neuvelia/agent-plane#84.

## Questions for Kacper (confirm before T3; T1 and T2 can start now)

ADR-0026 is complete on direction. These points are either factual drift since
it was written or gaps found while reading the code. Each has a recommended
answer; the plan assumes the recommendation.

1. **Schema number 12, not 11.** The ADR and spec say schema 11. F3.1 task 2
   took 11 (`events.SchemaVersion = 11`, `WorkspaceSchema = 11`, on `main`).
   `attempt_diff` becomes schema 12. The ADR/spec text is amended in T1/T6.
   *Recommend: yes.*
2. **ADR numbering vs PR #80** (see "Coordination"). *Recommend: #80 renumbers
   its two ADRs to 0027 and 0028; ADR-0026 stays.*
3. **Cap on listed files per attempt.** FR14 says a file beyond a budget is
   still listed with `omitted_reason: budget`, but a refactor can touch
   thousands of files, so event count would be unbounded. *Recommend: list the
   first 256 paths (sorted) per attempt; the rest are counted in the summary's
   `omitted_files`, so "not listed" never reads as "not changed". Amend FR14.*
4. **Redact file content before diffing, not the rendered patch.** A secret that
   spans lines never matches inside a patch (each line carries a `+`/`-`
   prefix). Redacting both sides first closes that leak and also makes the
   cut-straddling case trivial. Cost: a change from one secret value to another
   (both become the marker) shows as `modified` with no visible line change;
   hashes still differ. *Recommend: yes (secrets over completeness).*
5. **Ignored files.** The ADR says a changed ignored file is listed by path and
   never read. Detecting change without reading means comparing `Lstat`
   signatures (size, mtime, mode). Listing every ignored file in a tree such as
   `node_modules` is expensive. *Recommend: list ignored files without
   `--directory`, `Lstat` only, cap 20,000 entries; over the cap the summary
   carries `diagnostic_code: E_DIFF_IGNORED_LIMIT` and ignored detection is
   skipped. Edits that keep size, mtime and mode are not detected.*
6. **Scratch copies.** Dirty/untracked before-content is copied to a private
   directory. *Recommend: `os.MkdirTemp` under the OS temp dir (0700, files
   0600, keyed by SHA-256), removed when the run ends, total cap 64 MiB (beyond
   it a file is listed with hashes and `omitted_reason: budget`). A crash can
   leave a directory behind (no cleanup tool until F3.2/F3.11). The alternative,
   a directory under `--store-dir`, would show up beside run directories.*
7. **When capture runs.** FR12 says "after each producer finishes". A producer
   that errored or was cancelled has no `node_evidence` and the node ends
   (`execution_error`/`cancelled` are terminal, no retry follows). *Recommend:
   capture only when the producer returned without error, matching
   `node_evidence`; no `attempt_diff` events otherwise.*
8. **`git cat-file blob` per file, not `cat-file --batch`.** `--batch` needs
   stdin, which `gitstate.Run` does not support; changing that shared runner
   would touch the file F3.1 also edits. Only changed clean tracked files need a
   read, so process count stays small. Neither mode applies filters.
   *Recommend: per-blob now, `--batch` only if profiling asks for it.*
9. **Summary event shape.** Emitted last (totals are known only after
   budgeting), with `files`, `omitted_files`, `diagnostic_code` from the ADR plus
   `added_lines`, `deleted_lines`, `patch_bytes` totals and `omitted_reason:
   unreadable` when `diagnostic_code` is set. Closed diagnostic vocabulary:
   `E_DIFF_GIT_UNAVAILABLE`, `E_DIFF_GIT_FAILED`, `E_DIFF_TIMEOUT`,
   `E_DIFF_SCRATCH`, `E_DIFF_IGNORED_LIMIT`, `E_DIFF_UNSUPPORTED_ENTRY`.
   *Recommend: yes.*
10. **Scope of this issue.** #84 lists engine behavior only. The Compare diff
    panel (FR17) and the desktop setting (FR10, second half) are a follow-up
    issue; this plan adds only the desktop reducer tolerance test.
    `serve --redact-env` already exists on `main`, so that precondition of D3 is
    met. *Recommend: split.*

## Current state (verified in the tree at cc8e018)

- `events.SchemaVersion = 11`; `Event.Validate` is the type/schema gate
  (`internal/domain/events/event.go`); payload files per feature
  (`workspace.go`, `value.go`).
- Per attempt the coordinator emits `gate_check`, `return_envelope`,
  `node_evidence` (Agent only, paths only), then `attempt_finished`
  (`internal/runtime/accept.go`). `cycle()` in `internal/runtime/loop.go` runs
  the producer then the embedded Gate in a worker and returns a `completedNode`.
  Baselines for the scope Gate are carried in `state.baselines`.
- `internal/gitstate` holds index entries and SHA-256 digests in memory and
  discards them; `gitstate.Run` is the only way Git starts (fixed environment,
  hooks and fsmonitor off, 5 s and 16 MiB defaults, stderr discarded, no stdin).
- `Options.RecordValues` (ADR-0023) is the template for an opt-in feature:
  coordinator-owned recorder, preflight validation, redactor reuse.
- The architecture test forbids `internal/runtime` importing adapters; the
  existing exceptions live in `internal/archtest/baseline.txt` (which F3.1
  extended for `worktree`). No file contents are recorded anywhere.

## Proposed change

1. `internal/domain/diff`: pure Myers line diff and bounded unified patch.
2. `events`: `attempt_diff` type, `AttemptDiffPayload`, schema 12, bounds.
3. `internal/adapter/attemptdiff`: content-aware snapshots, scratch store,
   snapshot-to-snapshot comparison into payload candidates.
4. `runtime`: port `AttemptDiffer` + `Options.AttemptDiffs`; capture in
   `cycle()`, emission and run budget in `accept.go`.
5. CLI/serve: `--record-diffs`.
6. Docs closure; ADR-0026 accepted.

## Global Constraints

- Implement only F2.8b engine capture. No desktop panel, no new HTTP route
  (D4), no Shell producer capture (D6), no native Windows Git discovery (D5), no
  retention tool.
- Without `--record-diffs` nothing changes: same events, hashes, scheduling,
  zero Git or filesystem work. Existing tests pass unmodified.
- Capture never decides execution (ADR decision 6): outcome, Gate verdict,
  retry, acceptance and `run_finished` are identical with and without it, even
  when capture fails.
- Git only through `gitstate.Run`; allowed subcommands are `ls-files`,
  `cat-file blob`, `rev-parse`. Never `diff`, `add`, `commit`, `stash`,
  `update-index`, `status`. Nothing is written into `.git`.
- Raw Git stderr, absolute local paths and repository text never appear in
  error messages or events outside the redacted, bounded patch.
- No sleeps as synchronization; temporary Git repositories only; `-race` with
  `-count=20` on concurrent tests.
- Each task ends with `make check`, `go test -race` for touched packages and a
  review of `git diff --stat`. Commits are local and green in the order below.

## Review Focus

- The secret never reaching any event, store line, live channel, CLI output or
  surviving scratch file (including multi-line, straddling and path cases).
- Bounds: CPU (diff edit-distance cap), memory (1 MiB per side), disk (scratch
  cap), event count and size.
- Hostile repositories: symlinks, odd paths, huge files, special files,
  `.gitattributes` and config drivers that must not run.
- Coordinator-only budget and order state; no goroutine mutates run budgets.
- Compatibility: schema 1-11 streams decode; schema 11 refuses `attempt_diff`.

## Coordination

- **Event schema (F3.1).** F3.1 owns schema 11 and its `workspace_*` /
  `source_workspace_changed` types, including remaining tasks T3-T5 (T3 is on
  `claude/f3-1-runtime`). Those tasks must not change `SchemaVersion`. T2 here
  changes only the constant (11 to 12), adds one type and one `Validate` case,
  so a merge conflict with F3.1 would be at most the adjacent lines in
  `event.go`. Any code or test that hardcodes `11` (grep before T2) is updated
  in the same commit.
- **Runtime files (F3.1 T3).** F3.1 T3 edits `loop.go` (effective workspace),
  `accept.go`, `batch.go`, `preflight.go`, `scheduler.go`, the same files T4
  edits. T1-T3 (new packages and events) do not touch them and can land first.
  Land T4 after F3.1 T3, or rebase onto it before review.
- **ADR numbers (PR #80).** PR #80 (`docs(release): REL1/REL2`, branch
  `claude/nice-noether-dbf5my`, mergeable: CONFLICTING) adds
  `docs/adr/0026-distribution-and-release-channels.md` and
  `0027-public-roadmap-status-feed.md` and edits `docs/adr/README.md` and
  `docs/features/README.md`. `main` already has
  `0026-attempt-diff-capture.md` (and 0025 capability report), so #80's 0026
  duplicates an existing number and its README rows clash. 0027 is free today.
  **Resolution:** keep ADR-0026 as attempt diff capture (merged, referenced from
  the F2.8 spec, issue #84, this plan and the README); #80 renames
  distribution to 0027 and roadmap feed to 0028 (and fixes its references in
  `REL1`/`REL2`), rebases and resolves the README rows. This plan creates no
  new ADR number (schema 12 is an amendment of ADR-0026), so it adds no
  conflict. Whoever merges second re-checks `docs/adr/` for free numbers.

## Task order and dependencies

```text
T1 domain/diff ──┐
T2 events (12) ──┴─> T3 adapter ──> T4 runtime ──> T5 CLI/serve ──> T6 docs/closure
                     (needs Q3-Q9)   (after F3.1 T3)
```

T1 and T2 are independent.

---

### Task 1: Pure line diff (`internal/domain/diff`)

**Branch:** `claude/f2-8b-diff-package`

**Files:** Create `internal/domain/diff/{doc,diff,unified}.go` and tests.

**Interface (indicative):**

```go
type Result struct {
    Patch                   string // unified hunks, 3 context lines, no file header
    Added, Deleted          int    // full counts, independent of truncation
    Truncated               bool
}
type Limits struct{ MaxPatchBytes, MaxEdits int }
var ErrTooLarge = errors.New("diff exceeds edit limit")
func Lines(ctx context.Context, before, after []byte, l Limits) (Result, error)
```

Rules: Myers O(ND) on `\n`-split lines; `\ No newline at end of file`
marker; CRLF is part of the line (bytes as stored); patch is cut at a line
boundary at or below `MaxPatchBytes` with `Truncated`; when the edit distance
exceeds `MaxEdits` return `ErrTooLarge` (the caller records `too_large`, no
patch); `ctx` is polled so a hostile file cannot pin a CPU; output is valid
UTF-8 whenever the input is.

- [ ] Write failing tests first, then implement.
- [ ] Add the package to the architecture test's domain layer by path (no
  baseline change; the package imports only the standard library).

**Tests:** `TestLinesGolden` (add, delete, replace, empty to non-empty and back,
no trailing newline on each side, CRLF, identical input yields empty patch and
zero counts), `TestLinesHunkMergingAndContext`, `TestLinesRoundTripProperty`
(seeded random edits; a test-local patch applier reproduces `after` from
`before`), `TestLinesCountsSurviveTruncation`, `TestLinesTruncatesAtLineBoundary`,
`TestLinesEditLimitReturnsErrTooLarge` (1 MiB of disjoint lines, bounded time),
`TestLinesHonoursContext`, `TestLinesMatchesGitReference` (compares counts with
`git diff --no-index --numstat` in a hardened, test-only invocation; skipped if
Git is absent).

---

### Task 2: Event `attempt_diff`, schema 12

**Branch:** `claude/f2-8b-events`

**Files:** Create `internal/domain/events/diff.go`, `diff_test.go`. Edit
`event.go` (constant, `Validate` case, decode, clone via `payload_clone.go`),
`internal/domain/events/AGENTS.md` if present, `docs/architecture.md` (event
list), `desktop/test` (one reducer test). Edit any test that asserts schema 11.

**Contract:**

| Field | Rule |
| --- | --- |
| envelope | `node_id`, `node_run_id`, `attempt_id` required; schema >= 12 |
| `path` | empty only on the summary; otherwise valid UTF-8, <= 1024 bytes, relative, no NUL, no `.`/`..`/empty/`.git` component |
| `change` | `added`, `modified`, `deleted`, `mode`; empty on the summary |
| `binary`, `redacted`, `truncated` | booleans |
| `before_sha256`, `after_sha256` | empty or 64 lowercase hex; empty on the side that does not exist |
| `before_size`, `after_size` | >= 0 |
| `added_lines`, `deleted_lines` | >= 0 |
| `patch` | <= 16 KiB, valid UTF-8; must be empty when `omitted_reason` is set |
| `omitted_reason` | `binary`, `too_large`, `ignored`, `budget`, `unreadable`, or empty |
| summary only | `files`, `omitted_files`, `added_lines`, `deleted_lines`, `patch_bytes`, `diagnostic_code` (closed set above); `unreadable` iff `diagnostic_code` set |

Constants: `AttemptDiffSchema = 12`, `MaxAttemptDiffPatchBytes = 16 << 10`,
`MaxAttemptDiffAttemptBytes = 256 << 10`, `MaxRunDiffBytes = 4 << 20`,
`MaxAttemptDiffFile = 1 << 20`, `MaxAttemptDiffFiles = 256`.

- [ ] Write failing tests first.
- [ ] Gate the type on schema 12 in `Validate` (schema 0 means current, as for
  other types); `SchemaVersion` becomes 12.
- [ ] Check every `switch` over `events.Type` in `cmd/agentplane/output.go`,
  `internal/api`, `internal/store` and `desktop/src/reducer.ts`: unknown types
  must be ignored or printed generically. Fix only what would break.
- [ ] Confirm the store and hub accept lines well above 16 KiB (JSON escaping
  can expand a patch several times); raise a documented limit only if one exists.

**Tests:** `TestAttemptDiffRoundTripAndBounds` (every bound, both sides),
`TestAttemptDiffRequiresSchema12`, `TestAttemptDiffRejectsUnknownReasonAndBadHash`,
`TestAttemptDiffRejectsHostilePaths` (`../x`, `a/.git/b`, NUL, absolute, 1025 B,
invalid UTF-8), `TestAttemptDiffPatchMustBeEmptyWhenOmitted`,
`TestAttemptDiffSummaryRules`, `TestAttemptDiffCloneIsDetached`,
`TestOlderSchemaFixturesStillDecode` (schemas 1-11, including a schema 11 stream
with `workspace_*` events), `TestSchema11RefusesAttemptDiff`, store JSONL
round-trip with a 16 KiB patch containing quotes and non-ASCII, desktop reducer
test: an `attempt_diff` event leaves the existing view model unchanged and does
not throw.

---

### Task 3: Capture adapter (`internal/adapter/attemptdiff`)

**Branch:** `claude/f2-8b-adapter` (depends on T1, T2; needs answers 3-9)

**Files:** Create `internal/adapter/attemptdiff/{doc,snapshot,scratch,compare}.go`,
tests, and `AGENTS.md` for the package. Edit `internal/archtest/baseline.txt`
(one line, `internal/adapter/attemptdiff -> agentplane/internal/gitstate : R2`,
mirroring the existing `worktree` line; no new violation class), `docs/codebase-map.md`.

**Interface (indicative; the runtime port in T4 is shaped to match):**

```go
type Recorder struct{ /* scratch root, git runner (injectable in tests) */ }
func Open(ctx context.Context) (*Recorder, error)            // creates scratch 0700
func (r *Recorder) Close() error                             // removes scratch
func (r *Recorder) Snapshot(ctx context.Context, workspace string) (Snapshot, error)
type Redactor interface{ Redact([]byte) (out []byte, changed bool) }
func (r *Recorder) Diff(ctx context.Context, before, after Snapshot, red Redactor) ([]events.AttemptDiffPayload, error)
```

**Snapshot mechanism** (one pass per snapshot):

1. `gitstate.CanonicalWorkspace` (absolute, exact worktree root, HEAD).
2. `ls-files --stage -z` (mode, blob id, stage; stage != 0 fails the capture;
   gitlink `160000` entries are listed as `unreadable`, not fatal);
   `ls-files --others --exclude-standard -z`; and, for ignored files only,
   `ls-files --others --ignored --exclude-standard -z` (cap per Q5).
3. For each non-ignored path: `gitstate.ConfinedPath`, `Lstat`, open, `Fstat`
   and `os.SameFile` (a path swapped for a symlink after `Lstat` is rejected),
   stream once computing SHA-256 and the Git blob id (object format taken from
   the index blob id length; SHA-1 here is Git's object identity, not a
   security use, annotated for the linter). Special files fail that entry with
   `unreadable`. A symlink is recorded by target text, never followed.
4. Content retention: a tracked file whose working blob id equals its index blob
   id is *recoverable* (before-content later read with `cat-file blob`, which
   applies no filter). Anything else (dirty, eol or filter divergence,
   untracked) is copied to scratch when <= 1 MiB and text (no NUL in the first
   8 KiB, valid UTF-8), keyed by SHA-256, `O_EXCL` 0600 with temp-and-rename for
   concurrent writers. Binary and >1 MiB files keep hashes and sizes only.
5. Ignored files keep only the `Lstat` signature; contents are never opened.
6. A context deadline (default 60 s per snapshot) maps to `E_DIFF_TIMEOUT`.

**Compare:** union of paths; entries differing by SHA-256 or exec bit are
changes; classify `added`, `deleted`, `modified`, `mode`; renames are
deletion plus addition (ADR-0016). Sorted by path. Redact both sides'
text (Q4) and the path, render with `diff.Lines` (limits: 16 KiB patch, edit cap
from T1), build payloads, apply the 256 KiB per-attempt budget and the
256-file list cap, set `omitted_reason`, `redacted`, `truncated`, and sizes of
the original (unredacted) bytes with hashes of the original bytes (hashes
identify content, not reveal it; a known-secret preimage attack is out of scope
and noted in the ADR consequences). Git failures return a typed error carrying
only a diagnostic code from the closed set.

- [ ] Write the failing tests below first (they also pin the injected runner).
- [ ] Implement snapshot, scratch, compare. Keep one exported surface.
- [ ] Write the package `AGENTS.md` and codebase-map row.

**Tests (temporary Git repos, no Codex):** attribution
- `TestDiffTrackedModifiedStagedUntrackedDeletedAndModeOnly`
- `TestDiffRenameIsDeletionPlusAddition`
- `TestDiffExcludesPreExistingDirtUntouchedByTheAttempt`
- `TestDiffTwoSnapshotsOnlyContainTheLaterChange` (a.txt edited twice: the second
  diff is from the first result, not from HEAD)
- `TestDiffEolAndCleanFilterDivergenceFallsBackToScratchCopy`
- `TestDiffSymlinkComparesTargetTextAndNeverFollows`

limits and omission
- `TestDiffBinaryAndNonUTF8HashesOnly`, `TestDiffTwoMiBTextTooLargeHashesOnly`,
  `TestDiffEditLimitIsTooLarge`
- `TestDiffPerAttemptBudgetOmitsRemainderAsBudget` (summary count equals the
  omitted number), `TestDiffFileListCap` (300 files: 256 events plus counted rest)
- `TestDiffScratchTotalCapOmitsAsBudget`
- `TestDiffIgnoredChangedFileListedAndNeverRead` (ignored file with mode 000 or an
  unreadable one; capture still succeeds), `TestDiffIgnoredListLimit`
- `TestDiffCancelledContextReturnsWithoutPartialPayloads`

secrets
- `TestDiffRedactsSecretAcrossLinesPathAndCutBoundary` (secret straddling the
  16 KiB boundary and a multi-line secret; no secret in any payload field;
  `redacted: true`), `TestDiffRedactedTextStillValidUTF8`

safety
- `TestOnlyAllowlistedGitSubcommands` (injected recording runner: only
  `ls-files`, `cat-file blob`, `rev-parse`)
- `TestNoHookFilterTextconvDiffDriverOrFsmonitorRuns` (repository config and
  `.gitattributes` set `filter.x.clean/smudge/process`, `diff.x.textconv`,
  `diff.external`, `core.fsmonitor`, `core.hooksPath`, `core.attributesFile` to
  scripts that write a marker; no marker exists afterwards)
- `TestCaptureWritesNothingIntoGitDir` (digest of every file under `.git` by
  path, content and mtime is equal before and after, in a normal repo and in a
  linked worktree where `.git` is a file)
- `TestScratchPermissionsAndRemovedOnClose` (0700 directory, 0600 files, gone
  after `Close`, also after a failed `Diff`)
- `TestHostilePathsAndSymlinkSwapRejected` (symlinked ancestor, swap between
  `Lstat` and open, `.git` component, non-UTF-8 name)
- `TestGitFailureClassesMapToDiagnosticCodes` (RunStart gives
  `E_DIFF_GIT_UNAVAILABLE`, timeout gives `E_DIFF_TIMEOUT`, others
  `E_DIFF_GIT_FAILED`; no raw stderr in any error string)
- `TestSnapshotWorksInLinkedWorktree`
- `-race -count=20` on `TestConcurrentSnapshotsShareScratchSafely`.

---

### Task 4: Runtime integration

**Branch:** `claude/f2-8b-runtime` (depends on T1-T3; rebase on F3.1 T3)

**Files:** Edit `internal/runtime/{scheduler,preflight,loop,batch,accept}.go`,
create `internal/runtime/attemptdiff.go` and tests, edit
`internal/runtime/AGENTS.md`, `docs/architecture.md`.

**Seams (verified):** `Options.RecordValues`/`preflightValueRecording`
(`values.go`), `cycle()` and `completedNode` (`loop.go`), per-node carried state
in `batchState` (`batch.go`, like `baselines`), ordered emission in `accept.go`.

- Port in `attemptdiff.go`, consumer-owned and nil-safe:
  `type AttemptDiffer interface{ Open(ctx) (DiffSession, error) }`, with
  `DiffSession{Snapshot, Diff, Close}` as above and `DiffSnapshot any`. Option
  `Options.AttemptDiffs AttemptDiffer` (nil means off, no behavior change).
- `diffRecorder` (coordinator-owned like `valueRecorder`): holds the session, the
  redaction template and the remaining run budget; `nil` when off.

**Commits (in order):**

- [ ] **4a Preflight and lifecycle.** With `AttemptDiffs` set: `Open` before
  `run_started` (failure is a `PreflightError`, nothing emitted); redactions must
  be valid UTF-8 with a UTF-8-safe marker (reuse the `values.go` check); `Close`
  in `finish()` on every exit path, including cancellation and panic recovery
  paths that already run `finish`.
- [ ] **4b Worker capture.** In `cycle()`: for a loop Agent producer, take the
  *before* snapshot at the first attempt (later attempts reuse the carried
  *after* snapshot of the previous attempt; if none, take a fresh one), run the
  producer, and when it returned without error and the context is live take the
  *after* snapshot before the Gate, then `Diff`. Store `work.diff`
  (`[]AttemptDiffPayload` plus the diagnostic) and `work.diffAfter`. Uses the
  node's effective `config.workspace`, so an F3.1 worktree is captured with no
  extra code. Capture errors are converted to a summary-only result; they never
  set `work.err`, never alter `work.check`/`work.outputs`.
- [ ] **4c Ordered emission and run budget.** In `acceptBatchResults`, after
  `node_evidence` and before `attempt_finished`, emit the file events then the
  summary, in sorted path order, applying the remaining run budget (a patch that
  does not fit becomes `omitted_reason: budget`, counted in the summary).
  Carry `diffAfter` for the next attempt in coordinator state. Never emit for
  attempts without capture.
- [ ] **4d Interaction with isolation.** After F3.1 T3 is merged: assert event
  order `node_evidence` -> `attempt_diff`* -> `attempt_finished` ->
  `workspace_integrated`, capture inside the worktree, and a clean base for
  attempt 1.

**Tests (runtime package with a fake `AttemptDiffer`, plus real-adapter
integration with the scripted adapter from F3.1/`adaptertest` that edits files):**

- `TestNoAttemptDiffWithoutOption` (demo graph streams byte-identical to
  today; compares with a golden captured on `main`)
- `TestAttemptDiffEventOrderAndAttribution` (attempt 1 edits `a.txt`, attempt 2
  edits `a.txt` and adds `b.txt`: attempt 2 events hold exactly attempt-2
  changes; a pre-existing dirty file is absent) (spec criteria 1-2)
- `TestAttemptDiffNeverAltersLoopSemantics` (run the same scripted scenario with
  capture off, capture on, and capture failing at before, after and Diff: events
  with `attempt_diff` removed, outcomes, verdicts, retry counts, accepted values
  and `run_finished` are equal; summary carries the diagnostic) (criterion 7)
- `TestAttemptDiffForcedRealGitFailure` (the fake agent deletes `.git` mid
  attempt; run ends exactly as without capture)
- `TestAttemptDiffSecretNeverInAnyEventOrPersistedLine` (configured secret
  written by the agent, also inside a path and across lines and the 16 KiB cut;
  scan `RunResult.Events`, the `LiveEvents` channel, the `Persist` sink and a
  real `events.jsonl`; `redacted: true`) (criterion 3)
- `TestAttemptDiffRunBudgetOmitsLaterFilesAsBudget` (summary counts equal)
  (criterion 5)
- `TestAttemptDiffSkippedWhenProducerErrorsOrContextCancelled`
- `TestAttemptDiffOnlyForLoopAgentProducers` (Shell loop, non-loop Agent, Gate:
  none) (FR11)
- `TestAttemptDiffOpenFailureIsPreflightErrorBeforeRunStarted`
- `TestAttemptDiffSessionClosedOnEveryExitPath` (scratch directory gone after
  success, failure, cancellation) (criterion 9, second half)
- `TestAttemptDiffUnderConcurrentLoopNodes` with `-race -count=20`
- After F3.1 T3: `TestAttemptDiffIsolatedWorktreeOrderAndCleanBase`.

---

### Task 5: CLI and serve

**Branch:** `claude/f2-8b-cli` (depends on T4)

**Files:** Edit `cmd/agentplane/{cli,serve}.go` and tests,
`cmd/agentplane/AGENTS.md`, `docs/features/F0.7-cli-runner.md` (only if it lists
flags), `docs/graph-spec.md` not needed.

- [ ] `--record-diffs` on `run` and `serve`, default false; update the usage
  string (`cli.go:60`); compose `attemptdiff.Open` into `runtime.Options` only
  when the flag is set. Output states once, on stderr, that file contents are
  recorded into events and any store.
- [ ] `serve`: the flag is fixed at startup in `serveRunOptions`; each run opens
  its own session (own scratch). Document that redaction applies through
  `--redact-env`.
- [ ] A run on a non-Git or unsupported platform still succeeds: the summary
  carries `E_DIFF_GIT_UNAVAILABLE` (FR18).

**Tests:** built-binary CLI test with the fake Codex used by the existing CLI
tests editing a temporary repository: with the flag, `events.jsonl` contains
`attempt_diff` and no configured `--redact-env` secret; without the flag, no
`attempt_diff` in the streams of the demo graphs (`.agents/graphs/*`)
(criterion 1); `TestRecordDiffsFlagDefaultsOffAndIsValidated`;
`serve_test` run via `POST /runs` with the flag; scratch directory absent after
the run; `go test -race ./cmd/...`.

---

### Task 6: Docs and closure

**Branch:** `claude/f2-8b-docs`

**Files:** Edit `docs/adr/0026-attempt-diff-capture.md` (schema 12 everywhere,
D5/Q-amendments from the answers above, status accepted, validation evidence),
`docs/adr/README.md`, `docs/features/F2.8-attempt-compare.md` (tick only the
criteria a named test proves, FR14 list cap), `docs/features/README.md`,
`docs/architecture.md`, `docs/codebase-map.md`, `AGENTS.md` (phase paragraph, one
sentence), package guides. Prepare `docs/validation/F2.8b-codex-diff-demo.md`
(recipe; execution may follow).

- [ ] Run the `code-review` skill on the combined diff; `make check`,
  `go test -race ./...`.
- [ ] Spec acceptance mapping, each criterion to its test:

| F2.8b criterion | Proved by |
| --- | --- |
| no `attempt_diff` without the flag | `TestNoAttemptDiffWithoutOption`, CLI demo-graph test |
| exactly attempt-2 changes, no pre-existing dirt | `TestAttemptDiffEventOrderAndAttribution`, `TestDiffExcludesPreExistingDirtUntouchedByTheAttempt` |
| secret never in events, `redacted: true`, straddling the cut | `TestDiffRedactsSecretAcrossLinesPathAndCutBoundary`, `TestAttemptDiffSecretNeverInAnyEventOrPersistedLine` |
| 2 MiB text and binary: hashes, sizes, reason, no patch | `TestDiffBinaryAndNonUTF8HashesOnly`, `TestDiffTwoMiBTextTooLargeHashesOnly` |
| per-attempt/per-run budgets, counts equal | `TestDiffPerAttemptBudget...`, `TestAttemptDiffRunBudget...` |
| ignored file listed, never read | `TestDiffIgnoredChangedFileListedAndNeverRead` |
| Git failure leaves semantics unchanged | `TestAttemptDiffNeverAltersLoopSemantics`, `TestAttemptDiffForcedRealGitFailure` |
| no hook/filter/textconv/driver | `TestNoHookFilterTextconvDiffDriverOrFsmonitorRuns`, `TestOnlyAllowlistedGitSubcommands` |
| compare view shows patches, "new in attempt 2" | follow-up desktop issue (not ticked here) |
| nothing written into `.git`, scratch removed | `TestCaptureWritesNothingIntoGitDir`, `TestAttemptDiffSessionClosedOnEveryExitPath` |
| schemas 1-10 (now 1-11) still decode | `TestOlderSchemaFixturesStillDecode`, `TestSchema11RefusesAttemptDiff` |

---

## State and data contracts affected

- Event schema 11 -> 12; one new type `attempt_diff`; older schemas decode
  unchanged; a schema 11 stream containing it is invalid.
- `runtime.Options.AttemptDiffs` (new, nil = off); CLI/serve flag
  `--record-diffs`. Graph schema, CLI result envelope version and HTTP routes
  are unchanged. No change to `gitstate.Run` or `internal/worktree`.
- New package layering: `domain/diff` (stdlib only), `adapter/attemptdiff`
  (imports `gitstate`, `events`, `domain/diff`; one baselined R2 line).
- `events.jsonl` can now contain repository text when the flag is set; scratch
  copies exist on disk for the run's lifetime.

## Risks and edge cases

- **Unknown secrets.** Redaction is explicit-list only; changed lines with other
  secrets are recorded. Mitigations: opt-in, ignored files never read, the flag
  prints a warning, no retention tool until F3.2 (documented in the ADR).
- **Snapshot cost** on large untracked, non-ignored trees (one read per file per
  snapshot); bounded by the 60 s snapshot deadline and `E_DIFF_TIMEOUT`.
- **Shared workspace.** Concurrent writers (another node, the editor) appear in
  an attempt's diff exactly as in ADR-0016 evidence. With F3.1 isolation this
  attribution is exact. The UI follow-up must say "workspace observation".
- **Redaction masks secret-to-secret edits** (Q4) and hashes of changed secret
  files are stored.
- **Clock and mtime-only ignored detection** (Q5) can miss a same-size,
  same-mtime edit.
- **Large intermediate memory**: 1 MiB per side, 256 KiB patch; bounded.
- **Scratch orphans** after a crash (Q6).
- **TOCTOU** between listing and reading: handled per entry (`SameFile`), a
  failing entry becomes `unreadable`, never aborts the run.
- **Eol/filter divergence** makes clean-looking tracked files take the scratch
  path; still correct.
- **Windows native**: `E_DIFF_GIT_UNAVAILABLE`; macOS Git shim path works as for
  `gitstate`. CI Linux is the reference; macOS and Windows runs are not covered
  by this plan.
- **Merge order** with F3.1 and PR #80 (see Coordination).

## Ambiguities in the specification (resolved above or in the questions)

- FR14 "still listed" versus unbounded file count (Q3).
- ADR "patch text passes the redactor" versus multi-line secrets (Q4).
- ADR "ignored listed by path" versus cost of detection (Q5).
- ADR scratch "under the store" versus the runtime knowing no store (Q6).
- FR12 on failed or cancelled producers (Q7).
- `cat-file --batch` versus the no-stdin runner (Q8).
- Summary field set and emission order (Q9).
- Symlinks and gitlinks: symlinks diff by target text with change `modified`
  (no extra flag); gitlinks are listed `unreadable`; unresolved index stages fail
  the whole capture with `E_DIFF_GIT_FAILED`.

## Definition of done

Every F2.8b engine criterion maps to a named, passing test (table in T6);
`make check` and `go test -race ./...` pass; the architecture test passes with
one added baseline line; docs, ADR-0026 (accepted, schema 12) and indexes are
consistent; the desktop panel, setting and a real-Codex demo run are recorded as
follow-ups.
