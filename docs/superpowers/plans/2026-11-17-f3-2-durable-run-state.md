# F3.2 Durable Run State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a run's durable state exactly its event log: nothing a client or
the scheduler saw is missing after `kill -9`, an unterminated run is reliably
`interrupted` (or `running` while its writer lives), listing 5,000 runs stays
fast, and retention is explicit and local.

**Architecture:** JSONL stays the only authoritative state. `internal/store`
(adapter layer) gains creation durability, an OS advisory run lock, a rebuildable
per-run `summary.json`, a `store.json` format/retention marker and a prune
operation. `internal/app/trace` (the Collector) stops live delivery after the
first persist error. `cmd/agentplane` adds `runs prune` and composes retention
at `serve` start. `internal/api` and `desktop/` only surface what the store now
derives. No new event type, no event schema change.

**Tech Stack:** Go 1.23, standard library plus `golang.org/x/sys` (already in
`go.mod` as `// indirect`; becomes a direct requirement), existing store,
trace, CLI, API and desktop reducer.

**Spec:** `docs/features/F3.2-durable-run-state.md` (PR #108).
**ADR:** `docs/adr/00XX-durable-run-state.md` (proposed; number is the next free
at merge, assigned with `scripts/new-adr.sh` conventions in Task 9).

Tarmind (in code today: agentplane) keeps this feature local-first: no
database, no service, no network.

## Global Constraints

- Implement only F3.2 as specified. No resume or recovery (F3.3), no
  cancellation/timeouts (F3.4), no artifact store, no export, no SQLite, no
  recorded `run_interrupted` event.
- **Event schema: none.** `events.SchemaVersion` is not touched by any task.
  Interrupted and snapshot state are derived, not recorded. Nothing in this plan
  may edit `internal/domain/events/event.go`.
- Existing behavior is byte-compatible: an ADR-0019 store (no `store.json`, no
  `summary.json`, no `run.lock`) lists and reads exactly as today, except that a
  log without a terminal event and without a live writer is still `interrupted`
  and a log with a live writer is now `running`.
- Layering (architecture test, `internal/archtest`): `internal/store` is an
  adapter. It may import `domain` and project-neutral packages and the
  `golang.org/x/sys` module; it must not import `app`, `runtime`, another adapter,
  `api` or `cmd`. `internal/app/trace` stays standard-library and domain only (R1
  and R4: no `os`). `internal/api` already imports `store` (baseline line
  `internal/api -> internal/store`); the baseline must not grow and no line is
  added. Prune and lock code therefore live in `store`, never in `api` or `trace`.
- Never `_ =` an error. Disposable files (snapshot) report failure through a
  typed non-fatal accessor, never by being swallowed.
- Tests prove ordering with channels and fault seams, not sleeps. The crash test
  uses `Process.Kill` after reading the child's own output.
- Each task is one commit and ends with `gofmt`, `go vet ./...`,
  `go test ./...`, `go test -race` on the touched packages, and a `git diff
  --stat` review. `make check` runs at the end of every task.
- Commit authorship is the configured identity; commit messages end with the
  `Co-Authored-By` attribution line the repository convention requires.

## Current state (read before editing)

All paths verified against this worktree (`main` at `e55af36` plus the spec).

- **Collector.** `internal/app/trace/collector.go`. `Append` calls `retain`
  (which calls `retainAlways` -> `record` -> `c.persist`) and only then
  `enqueueLive`. After the first persist error `record` stops calling the sink
  and sets `persistErr`, but `enqueueLive` still runs for the failing and every
  later event. This is the gap the spec names. `finalize` already makes the
  final facts replay-only (they are never queued live).
- **Persist wiring.** `internal/runtime/scheduler.go` (`newRunMachine`:
  `m.emitter.SetPersist(options.Persist)`; `snapshot` copies `PersistError()`).
  `Options.Persist` documents "a failure never stops the run".
- **CLI `run`.** `cmd/agentplane/cli.go` (`executeCommand`): `--store-dir` opens
  `openPersister(runStore, graphBytes)` (a `persister` seam over `*store.Writer`)
  and wires `opts.Persist = writer.Append`. A persist error does **not** cancel
  the run; the result is shown and the command exits `exitInternal` (1) with the
  run ID and partial log path. `--watch` renders live events, then replays
  `result.Events` at the end (`finishWatch`).
- **`serve`.** `cmd/agentplane/serve.go` (`starter`): the sink writes the store
  first, calls `stop()` (cancels the run context) on failure and only then
  forwards to the hub, so serve already persists before delivering and does stop
  the run. `internal/api/server.go` overlays `running` on list/get only while
  `Hub.Live` says the run is unfinished; `streamEvents` serves live runs from the
  hub and everything else from `Store.Read`, ending with `event: end`.
- **Store.** `internal/store/store.go`: `Writer.Append` (write, `Sync` for every
  non-progress type), `open` (`MkdirAll`, exclusive `Mkdir`, `os.WriteFile`
  graph.yaml with no sync, exclusive `O_APPEND` log), `Close`, `Check`, `List`
  (reads and fully parses every run), `Read` (accepts a final unterminated line
  if it parses; flags `Truncated` otherwise), `Summary`, `StatusInterrupted`,
  `StatusCorrupt`. Fault seam: the `logFile` interface (tests assign `w.file`).
  A future-schema event makes `Read` return a plain error (decoder text
  "unsupported event schema version N"); `List` turns any error into `corrupt`.
- **Tests that pin today's behavior** (must keep passing or be changed on
  purpose, named in the task that changes them): `internal/store/store_test.go`
  (`TestRunWithoutTerminalEventIsInterruptedAndEarlierEventsRead`,
  `TestTruncatedTailIsReportedNotFatal`, `TestReaderRejectsAnUnsupportedSchemaVersion`,
  `TestEmptyOrHeaderOnlyLogIsCorruptNotAnInterruptedRun`,
  `TestLifecycleEventsAreSyncedAndProgressEventsAreNot`,
  `TestWriterReportsAndLatchesWriteAndSyncFailures`),
  `cmd/agentplane/store_test.go` (`TestKilledRunIsInterruptedAndEarlierEventsStayReadable`,
  `TestPersistFailure*`), `internal/app/trace/collector_test.go`
  (`TestCollectorStopsPersistingAfterFirstErrorAndReportsIt`).
- **Desktop.** `desktop/src/reducer.ts`: `RunStatus` includes `interrupted`;
  `settle(run, live)` and `presentRun(run, endedNormally, lost)` derive it for
  a stream with no terminal event; `hooks.ts` calls `presentRun`. `NodeStatus`
  has no `interrupted`; a started node of a dead run keeps `running`.
  `status.tsx` holds exhaustive `NODE`/`RUN` tables; `i18n/en.ts` and `pl.ts`
  hold `status.node.*` / `status.run.*`. `types.ts` `RunSummary` has no
  `interrupted_at_sequence`.
- **Domain.** `internal/domain/run/state.go` has run states
  running/succeeded/failed/cancelled and node states without `interrupted`;
  `INTERRUPTED` is deliberately not added there (it is derived read state, not a
  scheduler transition). No Go event reducer exists in the repository.
- **Dependencies.** `go.mod` has `golang.org/x/sys v0.28.0 // indirect`;
  `go.sum` has its hashes. It provides `unix.Flock` and
  `windows.LockFileEx`/`UnlockFileEx`.

## Proposed change

1. Collector: no live delivery for the failing event or any later event once
   persistence has failed (events already persisted stay deliverable).
2. Writer: durable creation order (graph.yaml sync, then log, then fsync of the
   run directory and the store directory on POSIX), short-write detection, and
   the same behavior on Windows with directory sync documented as a no-op.
3. Reader: only `\n`-terminated lines count; a typed `UnsupportedSchemaError`
   and status `unsupported`; `store.json` format marker (absent = 0).
4. Run lock and the derived `running`/`interrupted` split, with
   `interrupted_at_sequence`.
5. Per-run `summary.json` snapshot (terminal only) and a `List` that trusts it
   under the spec's validation rule; 5,000-run benchmark.
6. Retention in `store.json` and `agentplane runs prune [--dry-run]`.
7. API/desktop: surface `interrupted_at_sequence`, `unsupported`, node-level
   interruption derived by the reducer.

## Files to modify or create

| Area | Files |
| --- | --- |
| Trace | edit `internal/app/trace/collector.go`, `collector_test.go` |
| Store | edit `internal/store/store.go`, `store_test.go`; create `internal/store/{format,lock,lock_unix,lock_windows,lock_other,syncdir_unix,syncdir_windows,snapshot,prune}.go` and matching `_test.go`; create `internal/store/testdata/schemas/v1.jsonl` ... `v11.jsonl` plus a generator note in the test; edit `internal/store/AGENTS.md` |
| CLI | edit `cmd/agentplane/{runs,serve,cli,json}.go` (prune, serve-start retention, usage string, `cliDocument`); create `cmd/agentplane/crash_test.go`, `prune_test.go`; edit `cmd/agentplane/store_test.go`, `AGENTS.md` |
| API | edit `internal/api/server.go` (no new route; status passthrough), `server_test.go` |
| Desktop | edit `desktop/src/{reducer,hooks,status,types}.ts(x)`, `i18n/{en,pl}.ts`; tests `desktop/test/reducer.test.ts` |
| Module | edit `go.mod` (drop `// indirect`; `go.sum` unchanged) |
| Docs | ADR, spec, `docs/domain-model.md`, `docs/architecture.md`, `docs/codebase-map.md`, `docs/roadmap.md`, `docs/features/README.md`, `docs/adr/README.md`, root `AGENTS.md` phase paragraph |

## Contracts affected

- Store layout adds `store.json`, `<run>/summary.json`, `<run>/run.lock`
  (all 0600, directories 0700). `events.jsonl` and `graph.yaml` are unchanged.
- `store.Summary` gains optional `last_sequence`, `min_schema`, `max_schema`,
  `interrupted_at_sequence`; `Status` gains `running` (live writer, read from
  the store) and `unsupported`; `interrupted` now means "no terminal event and
  no live writer".
- API `GET /runs`, `GET /runs/{id}`: additive fields only (they serialize
  `store.Summary` directly).
- CLI: new `runs prune [--store-dir DIR] [--dry-run] [--json]`; `runs list` shows
  `running`/`unsupported`.
- Events: unchanged. Event schema: **none**.

## Review Focus

- The persist-before-deliver ordering in `Collector.Append` and that the
  delivery stop does not drop an already persisted event or deadlock `finalize`.
- That the lock file descriptor is not inherited by node child processes
  (Go sets close-on-exec / non-inheritable handles; Task 5 proves it with a
  surviving orphan).
- Reader/writer races: never a torn event, never a rewrite of a snapshot while
  a writer holds the lock, prune never deleting a live or unterminated run.
- Windows semantics (below) and that no authoritative state depends on rename.

## Task order and dependencies

```text
T1 collector delivery stop ──────────────────────────────────┐
T2 creation durability ─> T3 reader hardening ─> T4 run lock ├─> T5 crash tests
                                                     │       │
                                                     ├─> T6 snapshot + fast List ─> T7 retention + prune
                                                     └─> T8 API + desktop
                                                                    T9 docs and closure (last)
```

T1 is independent and can ship first. T2-T4 and T6 touch only `internal/store`.
T5, T7 and T8 touch composition/CLI/desktop files that other in-flight work also
edits; see "In-flight branches" for the rebase points.

---

### Task 1: Stop live delivery after the first persist error

**Branch:** `claude/f3-2-delivery` (first, smallest, closes the gap named in the spec)

**Files:** Edit `internal/app/trace/collector.go`, `collector_test.go`; edit
`internal/runtime/scheduler.go` (only the `Options.Persist` doc comment);
edit `cmd/agentplane/store_test.go` only if an assertion needs the new fact.

- [ ] **Red first.** Add `TestLiveDeliveryStopsAfterPersistError` in
  `internal/app/trace/collector_test.go`: a collector with a live channel and a
  persist function that succeeds for N events then fails; a reader goroutine
  drains `live`. Append M > N events, finalize, wait for the pump (the
  collector's own `done` via `Append(run_finished)` returning), then assert the
  reader received exactly the first N events and neither the failing event nor
  any later one. Current `main` delivers all M: record the failure.
- [ ] Add `TestCollectorDeliversOnlyPersistedEvents`: the persist function
  records into a mutex-guarded slice and blocks on a per-event channel; the live
  consumer asserts, for every received event, that it is already in the persisted
  slice. Releases are channel-driven, no sleeps.
- [ ] Implement in `Append`: after `retain`, call `enqueueLive` only when
  `c.persistErr == nil`; otherwise count the event as a live omission
  (`stats.LiveStopped = true`, `LiveOmittedEvents++`, bytes) exactly like the
  bound-exceeded path, so `progress_omitted` stays truthful. Events queued before
  the error remain in the FIFO and are still delivered (they are durable).
  Keep: progress beyond the retention bound is still delivered live while
  persistence is healthy (spec exception), and a collector without `SetPersist`
  is unchanged.
- [ ] Update the `Options.Persist` comment in `scheduler.go` (a failure does not
  stop the run in the scheduler; subscribers stop receiving at the first error;
  `serve` additionally cancels). Touch no other line of `scheduler.go` (F3.1
  edits the same struct; see rebase notes).
- [ ] Check the existing CLI test `TestPersistFailureInWatchModeFinishesTheWatchAndReports`
  still passes unchanged (the watch finishes, exits 1, stderr reports the
  failure). If it must change, assert that no event after the failing one was
  rendered live; the end-of-run replay of in-memory facts is unchanged (open
  question 2).

**Tests:** the two above plus the existing collector suite, `go test -race
./internal/app/trace/... ./internal/runtime/...`.

**Commit:** `fix(trace): stop live delivery after the first persist error (F3.2 task 1)`.

---

### Task 2: Creation durability and write fault seams

**Files:** Edit `internal/store/store.go`, `store_test.go`; create
`syncdir_unix.go`, `syncdir_windows.go`, `syncdir_test.go`.

- [ ] **Red first**, in `store_test.go`, using a fault-recording `fsOps` seam
  (unexported field on `Store`; keyed `store.Store{Dir: ...}` literals elsewhere
  keep compiling):
  `TestCreationOrderSyncsGraphBeforeLogThenDirectories` (recorded sequence:
  mkdir run dir, write+sync graph.yaml, create log, sync run dir, sync store
  dir; on Windows the two directory syncs are absent),
  `TestRunDirectoryWithoutAValidFirstLineIsNeverHalfReadable` (a directory with
  `graph.yaml` only, an empty log, and a log with one partial line each list as
  `corrupt` and are skipped by `Read`),
  `TestInjectedFsyncFailureStopsTheRunAndLeavesAReadablePrefix` (sync fails on
  the third lifecycle event: `Append` errors, later `Append` returns the latched
  error, `Read` returns the first two events and status `interrupted`),
  `TestShortWriteStopsTheRunAndLeavesAReadablePrefix` (`Write` returns
  `n < len, nil`; the writer must treat that as `io.ErrShortWrite`; today
  `Append` ignores `n`; the torn tail is `truncated`, the prefix is readable).
- [ ] Implement: `open` writes `graph.yaml` through a create/write/`Sync`/close
  helper, then creates the log, then calls `syncDir(runDir)` and
  `syncDir(storeDir)`. Check `n != len(line)` in `Append`.
  `syncdir_unix.go` opens the directory read-only and `Sync`s it, treating
  `EINVAL`/`ENOTSUP` from filesystems that do not support it as success.
  `syncdir_windows.go` is a documented no-op (directory handles cannot be
  flushed with `FlushFileBuffers`; `File.Sync` on the log maps to
  `FlushFileBuffers`, which is what lifecycle events rely on).
- [ ] Update the "Durability scope" comment in `store.go` to the new truth.

**Windows:** `TestCreationOrder...` is OS-aware (expects no directory sync);
`syncdir_test.go` asserts the Windows function returns nil without opening the
directory and the unix one syncs a real temp directory.

**Commit:** `feat(store): make run creation durable and detect short writes (F3.2 task 2)`.

---

### Task 3: Reader hardening, typed unsupported schema, `store.json`

**Files:** Edit `internal/store/store.go`, `store_test.go`; create `format.go`,
`format_test.go`, `internal/store/testdata/schemas/v1.jsonl` ... `v11.jsonl`.

- [ ] **Red first:**
  `TestReaderConsumesOnlyNewlineTerminatedLines` (a final valid-JSON line
  without `\n` is not an event and, for a run with no live writer, sets
  `Truncated`; today `Read` accepts it), `TestPartialTrailingLineOfALiveRunIsIgnored`
  (needs the lock from Task 4, so it lands there; here the case is covered by
  the dead-run assertion), `TestUnsupportedSchemaIsTypedAndListedUnsupported`
  (`errors.As` to `*UnsupportedSchemaError{Version}`; `List` shows
  `Status: "unsupported"` with the schema in `Error`; the files are
  byte-identical afterwards), `TestSchemaFixturesRoundTrip` (every
  `testdata/schemas/vN.jsonl` for N in 1..`events.SchemaVersion` reads back; a
  generated fixture at `events.SchemaVersion+1` is `unsupported`, never a literal
  `12`; the test also fails if a fixture for the current version is missing, so
  a future schema bump must add its fixture), `TestStoreJSONIsCreatedLazilyOnFirstWrite`,
  `TestAbsentStoreJSONIsFormatZeroAndReadable`, `TestNewerStoreFormatIsReadOnlyWithAnActionableError`
  (`Check` and `NewWriter.Append` fail with `*FormatError`; `List`/`Read` still
  work), `TestConcurrentFirstWritersCreateStoreJSONOnce` (temp file + sync +
  rename, tolerate an existing file).
- [ ] Implement: `UnsupportedSchemaError` is store-local. On an unmarshal error
  the reader peeks `event_schema_version` and, when greater than
  `events.SchemaVersion`, returns the typed error (this deliberately avoids
  editing `internal/domain/events/event.go`, which F2.8b also edits). A lenient
  header peek supplies `run_id`, `graph_id` and timestamp so an unsupported run
  still sorts and displays. `Summary` gains `LastSequence`, `MinSchema`,
  `MaxSchema` (omitempty). `format.go` reads `store.json`
  (`{"store_format":1,"retention":{...}}`; unknown fields tolerated, negative
  retention values rejected) and creates it lazily from `open`.
- [ ] Change `TestReaderRejectsAnUnsupportedSchemaVersion` to assert the typed
  error (it currently only asserts `err != nil`).
- [ ] Fixtures: minimal hand-written logs (run_started, node_started,
  node_finished, run_finished, plus the schema 1 `node_evidence` and schema 4
  gate shapes the decoder upcasts); documented in the test file header. No
  fixture is rewritten by tests.

**Commit:** `feat(store): typed unsupported schema, newline-complete reads and store format marker (F3.2 task 3)`.

---

### Task 4: Run lock and derived `running` / `interrupted`

**Files:** Edit `internal/store/store.go`, `store_test.go`, `go.mod`; create
`lock.go`, `lock_unix.go`, `lock_windows.go`, `lock_other.go`, `lock_test.go`.

- [ ] **Red first:**
  `TestWriterHoldsTheRunLockUntilClose` (a second open of `run.lock` in the same
  process cannot take it; after `Close` it can), `TestLiveRunReadsRunningAndDeadRunReadsInterrupted`
  (status table: terminal event -> its status; no terminal and lock held ->
  `running`; no terminal and lock free -> `interrupted` with
  `InterruptedAtSequence` = last durable sequence), `TestRunLockIsReleasedWhenTheWriterIsAbandoned`
  (drop the writer without `Close`, close its lock file as process death does;
  `List` reports `interrupted`; the real process-death proof is Task 5),
  `TestLockedRunWithNoValidFirstLineIsSkippedNotCorrupt` (a writer between
  `Mkdir` and the first event is "starting"), `TestListNeverSeesATornEventWhileAppending`
  (a writer goroutine appends several hundred events including progress; the
  reader goroutine loops `Read` until it observes `run_finished`, asserting every
  result is a valid prefix with increasing sequence; synchronized by the
  terminal event, no sleeps; runs under `-race`), `TestPartialTrailingLineOfALiveRunIsIgnoredNotTruncated`,
  `TestSecondWriterRefusesAnExistingRunDirectory` (existing exclusive-`Mkdir`
  behavior; the spec's "lock cannot be taken" error is the same actionable
  message plus a seam test where `run.lock` already exists locked).
- [ ] Implement: `open` takes the exclusive non-blocking lock on `run.lock`
  (created 0600, empty) right after the exclusive `Mkdir`, before `graph.yaml`.
  Unix: `unix.Flock(fd, LOCK_EX|LOCK_NB)`; reader probe `LOCK_SH|LOCK_NB`
  (`EWOULDBLOCK` means held). Windows: `windows.LockFileEx` on byte range [0,1)
  with `LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY` for the writer and
  `LOCKFILE_FAIL_IMMEDIATELY` alone for the probe (`ERROR_LOCK_VIOLATION` means
  held); release with `UnlockFileEx`, and the kernel releases it on process
  termination. `lock_other.go` (neither unix nor windows) returns a typed
  `ErrLockUnsupported` so the build is explicit. The reader opens `run.lock`
  read-only and never creates it; a missing file means no live writer (format 0
  runs and runs from a binary older than this change).
- [ ] `Read` derives status after parsing: terminal event wins; otherwise
  `running` if the probe says held, else `interrupted` with
  `InterruptedAtSequence = LastSequence`. `Truncated` is reported only for a
  dead run.
- [ ] `go.mod`: change `golang.org/x/sys v0.28.0 // indirect` to a direct
  requirement (comment removed; `go.sum` already has both hashes; verify with
  `go mod verify` and `go build ./...` on Linux and `GOOS=windows go vet
  ./internal/store/...`). Explain the dependency in the ADR (standard `syscall`
  has no `LockFileEx`; runtime dependency; already vendored by the module graph).
- [ ] Confirm `internal/archtest` passes and `baseline.txt` is unchanged.

**Windows:** `lock_test.go` runs on both OSes (same-process second handle
conflicts on both: `flock` is per open file description, `LockFileEx` per
handle). Lock files are never read, so mandatory byte-range locking cannot
block readers of `events.jsonl`.

**Commit:** `feat(store): run lock and derived running/interrupted status (F3.2 task 4)`.

---

### Task 5: Crash and persist-before-deliver tests (`kill -9`)

**Files:** Create `cmd/agentplane/crash_test.go`; edit `cmd/agentplane/AGENTS.md`
and `internal/store/AGENTS.md` pointers.

Most kill assertions already hold on `main` (the log keeps lifecycle events, a
log without a terminal is `interrupted`). This task is a regression pin plus the
cross-process proof of the lock: the first assertion that is red on `main` is
that a **live** run in another process reads `running` (red until Task 4).
State this honestly in the commit body.

- [ ] Helper process, following the existing `TestCLIHelperProcess` pattern
  (`AGENTPLANE_CLI_HELPER`, `os.Args[0]`, `@TEST_BIN@` placeholder in
  `graphFile`): `TestCrashHelperProcess` runs `runCommand` with the arguments
  after `--` and the real stdout, using a two-branch Shell graph: `fast` runs
  the existing `echo` helper mode and finishes; `slow` runs a new helper mode
  `block` that prints `child-ready:<pid>` (as the existing `wait` mode does) and
  then blocks forever on a read from a pipe nobody writes. Nothing polls.
- [ ] `TestKillNineAfterNodeFinishedLeavesDurablePrefix`: start the child with
  `run GRAPH --store-dir DIR --watch`, read its stdout line by line until the
  renderer's node-finished line for `fast` (the blocking read is the
  synchronization), assert via `runs list --json` in the test process that the
  run is `running` (child alive), then `Process.Kill()` and `Wait()`. Then assert
  in a fresh invocation: `runs list` reports `interrupted`; `runs show` contains
  every event up to and including `fast`'s `node_finished` and no `run_finished`;
  `events.jsonl` ends on a `\n`-terminated line; `slow` has `node_started` and no
  terminal event. The orphaned `block` grandchild is still alive at this point
  (killed in `t.Cleanup` from the pid found in the stored `log_chunk`), which
  proves the lock descriptor is not inherited by node child processes.
- [ ] `TestNoStreamedNonProgressEventIsMissingFromDiskAfterKill`: a second
  helper mode composes `runtime.Run` with `store.Writer` and prints each live
  event as one JSON line; the test kills the child after the first
  `node_finished` line and asserts that every non-progress line printed so far
  has the same sequence, type and payload in `events.jsonl` (progress and
  `log_chunk` excepted, spec requirement 1).
- [ ] `TestStoredEventsEqualLiveEventsForAFinishedRun` (spec criterion "reduced
  from stored equals reduced from live"): there is no Go event reducer in the
  repository, so the test proves the stronger statement that the stored event
  sequence equals the live sequence (`result.Events` vs `store.Read(...).Events`,
  normalized through JSON), for the demo graph with approvals. The desktop half
  is Task 8 (the reducer is a pure function of those events).
- [ ] All tests run on Windows: `Process.Kill` is `TerminateProcess`; the pid
  cleanup uses `os.FindProcess(pid).Kill()`. Gate and Agent nodes are not used,
  so Windows-only skips for Git/Codex do not apply.

**Commit:** `test(cli): kill -9 crash recovery and persist-before-deliver (F3.2 task 5)`.

---

### Task 6: Per-run summary snapshot and fast `List`

**Files:** Create `internal/store/snapshot.go`, `snapshot_test.go`,
`list_bench_test.go`; edit `store.go`, `store_test.go`; edit
`docs/features/F3.2-durable-run-state.md` (benchmark section, amendment A3).

- [ ] **Red first:**
  `TestListParsesNoLogWhenTheSnapshotIsValid` (a test hook counts parsed logs),
  `TestStaleOrDeletedSnapshotNeverChangesListOutput` (golden: `List` equals the
  result with all snapshots removed, with each snapshot truncated, with a wrong
  `log_bytes`, with a wrong run ID, and with garbage JSON),
  `TestSnapshotIsIgnoredWhileTheRunIsUnterminated`,
  `TestSnapshotWriteIsAtomicAndSyncedAndLogWinsOnDisagreement` (fault seam on
  rename/sync),
  `TestListRewritesAStaleSnapshotBestEffortAndSkipsWhileLocked`,
  `TestReadOnlyStoreStillListsWithoutRewriting` (directory made unwritable;
  skipped on Windows where mode bits do not apply),
  `TestSnapshotFailureIsReportedNotFatal` (`Writer.SnapshotError()` returns a
  typed error; the run result is unaffected),
  `TestListFiveThousandRunsMeetsTheBudget` (Linux gate: 1 s warm, skipped under
  `-short` and `-race`; Windows number logged, not gated) and
  `BenchmarkList5000`.
- [ ] Implement: `Writer` tracks `graph id/version`, start time, status, event
  count, last sequence, schema range and bytes written, and on the terminal
  event, after the log sync, writes `summary.json` (temp file in the run
  directory, `Sync`, `os.Rename`; failure recorded in `SnapshotError()`).
  `List` reads a run's `summary.json` when present, stats `events.jsonl`, and
  trusts the snapshot only if the run ID matches, the status is terminal and
  `log_bytes` equals the file size; any other case parses the log (`Read`) and
  best-effort rewrites the snapshot under the exclusive try-lock (skipped when
  the lock is held). Seeding for the benchmark writes synthetic terminal logs and
  snapshots directly (no fsync per file) in a test helper.
- [ ] Spec amendment A3: the snapshot is written at the terminal event only. A
  `run_started` snapshot can never be trusted for listing (non-terminal), so it
  would cost a rename per run for no reader. Update spec requirement 5
  accordingly.
- [ ] No store-wide `index.json`. If the benchmark misses the budget, add a task
  here before merging (rebuildable index with the same validation rule); do not
  add it speculatively.
- [ ] Record the measured numbers (Linux gated, Windows informational) in the
  spec's new "Benchmark" section.

**Windows:** `os.Rename` replaces an existing file but may fail with a sharing
violation if a reader holds it; the snapshot is rebuildable, so that is a
reported, non-fatal result. No authoritative state relies on rename.

**Commit:** `feat(store): rebuildable per-run summary snapshot and fast listing (F3.2 task 6)`.

---

### Task 7: Retention and `runs prune`

**Files:** Create `internal/store/prune.go`, `prune_test.go`; edit
`internal/store/format.go`; edit `cmd/agentplane/{runs,serve,cli,json}.go`;
create `cmd/agentplane/prune_test.go`; edit `cmd/agentplane/AGENTS.md`.

- [ ] **Red first**, store level: `TestPruneNeverRemovesAnUnterminatedRunWhateverItsAge`
  (live and interrupted runs older than every limit survive),
  `TestPruneHonoursMaxRunsMaxAgeAndMaxTotalBytes` (table, each limit alone and
  combined, injected clock, oldest first),
  `TestPruneDryRunDeletesNothingAndReportsTheSamePlan`,
  `TestPruneNeverRemovesALockedCorruptOrUnsupportedRun`,
  `TestPruneSkipsForeignDirectoriesAndSymlinks` (only valid run-ID directories
  that contain `events.jsonl`; a symlink named like a run ID pointing outside the
  store is never followed or deleted), `TestPruneContinuesAfterOneFailure`
  (fault seam on removal; the others are removed, the failure is reported with
  the run ID, the result is a non-nil aggregate error),
  `TestNoRetentionConfiguredMeansPruneDoesNothing`, `TestRetentionValuesAreValidated`.
- [ ] CLI: `TestRunsPruneCommand` (human and `--json` output list removed run
  IDs; exit 0 on success, 1 when any deletion failed, 2 on usage),
  `TestServeStartPrunesOnlyWhenRetentionIsConfigured`.
- [ ] Implement `Store.Prune(PruneOptions{DryRun, Now})`: classify each run
  (snapshot-aware, same trust rule as `List`), take the exclusive try-lock before
  deleting (a held lock means skip), delete files then the directory, release
  the lock handle before removing `run.lock` and the directory. Selection:
  terminal runs only; `max_age_days` first, then `max_runs` counted over all
  runs (non-deletable runs consume the budget), then `max_total_bytes` over run
  directory sizes, always oldest first. Prune does not touch Git worktrees or
  `refs/agentplane/*` (F3.1 owns those) and says so in `--help`/docs.
- [ ] `runs prune` parses flags like `runs list`; `serve` calls `Prune` once at
  start only when `store.json` configures retention, and reports to stderr
  (stdout stays the single port/token line).

**Windows:** a removal that fails on a sharing violation is reported and
skipped; Windows-only test holds a run's log open and asserts that run is
reported, not retried. A Unix fault-seam test covers the same logic.

**Commit:** `feat(store): explicit retention and runs prune (F3.2 task 7)`.

---

### Task 8: API and desktop surface

**Files:** Edit `internal/api/server.go`, `server_test.go`;
`desktop/src/{reducer.ts,hooks.ts,status.tsx,types.ts}`, `desktop/src/i18n/{en,pl}.ts`,
`desktop/test/reducer.test.ts` (new fixture-based cases), Storybook/e2e only if
`npm run check` says a snapshot changed.

- [ ] **Red first**, Go: `TestRunSummaryCarriesInterruptedAtSequence`,
  `TestUnsupportedRunIsListedAndGetReportsIt` (typed `unsupported` with a stable
  error code, never 500), `TestHubOverlayDoesNotMaskARunLiveInAnotherProcess`
  (store says `running`; the hub overlay never downgrades it to `interrupted`).
  No new route; `Summary` already serializes through `listRuns`/`getRun`.
- [ ] Desktop red: `reducer.test.ts` cases `derives interrupted nodes and
  attempts once the run is interrupted` (a node with `node_started` and no
  terminal event, a `retrying` node and a `waiting_human` node all become
  `interrupted`; a finished node is untouched; a still-followed run keeps
  `running`), `uses the store status for a run live in another process` (summary
  `running` suppresses `settle`), and a golden-log equivalence case reducing the
  stored sequence and the same sequence delivered as a stream.
- [ ] Implement: add `interrupted` to `NodeStatus`; `settle` maps
  `running|retrying|waiting_human` nodes to `interrupted` when it marks the run
  interrupted; `presentRun` takes an optional `storeStatus` and does not settle a
  run the store reports as `running` (`hooks.ts` passes the summary status;
  today a no-terminal log served from the store is always settled, which would
  mislabel a run live in another process). Extend `status.tsx` tables and the
  `en`/`pl` message keys (`status.node.interrupted`, `status.run.unsupported`).
  `RunSummary` gains `interrupted_at_sequence?`.
- [ ] `cd desktop && npm run check`; `make desktop-e2e` and `make desktop-stories`
  if the status tables are covered.

**Commit:** `feat(api,desktop): surface interrupted_at_sequence, unsupported runs and interrupted nodes (F3.2 task 8)`.

---

### Task 9: Docs, ADR number and closure

**Files:** Rename `docs/adr/00XX-durable-run-state.md` to the next free number
at merge (currently `0027`, but follow `docs/adr/README.md` at merge time; F3.4
and F3.8/F3.9 also hold `00XX` ADRs), update `docs/features/F3.2-durable-run-state.md`,
`docs/features/README.md`, `docs/adr/README.md`, `internal/store/AGENTS.md`,
`docs/domain-model.md` (interrupted is a derived read state; state wording),
`docs/architecture.md` (replace the "future durable event store" sentence and the
durability paragraph), `docs/codebase-map.md` (store files, `golang.org/x/sys`),
`docs/roadmap.md`, root `AGENTS.md` ("Current product phase").

- [ ] Tick spec acceptance criteria only with a named passing test (table below).
- [ ] Spec amendments recorded in the spec, not silently: A1 (CLI `run` does not
  cancel on persist error), A2 (end-of-run watch replay), A3 (snapshot at terminal
  only), A4 (no Go reducer), A5 (schema fixture count), A6 (retention is edited by
  hand).
- [ ] Update ADR-0019 "Durability scope" with a pointer to the new ADR.
- [ ] Final review pass with the `code-review` skill, then `make check`,
  `go test -race ./...`, `cd desktop && npm run check`, and the Windows store/CLI
  subset.

**Commit:** `docs(store): close F3.2 durable run state (F3.2 task 9)`.

---

## Acceptance criteria to test mapping

| Criterion | Test (task) |
| --- | --- |
| SIGKILL after observed `node_finished` keeps it; fresh process says `interrupted` | `TestKillNineAfterNodeFinishedLeavesDurablePrefix` (T5) |
| No printed/streamed non-progress event missing from disk | `TestNoStreamedNonProgressEventIsMissingFromDiskAfterKill`, `TestCollectorDeliversOnlyPersistedEvents` (T5, T1) |
| Live delivery stops after persist error | `TestLiveDeliveryStopsAfterPersistError` (T1) |
| Prune never removes unterminated run | `TestPruneNeverRemovesAnUnterminatedRunWhateverItsAge` (T7) |
| Stored state equals live state; desktop reducer | `TestStoredEventsEqualLiveEventsForAFinishedRun` (T5), reducer golden case (T8) |
| Another process reads `running`, then `interrupted` after kill | T5 kill test plus `TestLiveRunReadsRunningAndDeadRunReadsInterrupted` (T4) |
| Schemas 1-11 read; newer is `unsupported` and unmodified | `TestSchemaFixturesRoundTrip`, `TestUnsupportedSchemaIsTypedAndListedUnsupported` (T3) |
| 5,000-run listing within budget, no log parsed when snapshot valid | `TestListFiveThousandRunsMeetsTheBudget`, `TestListParsesNoLogWhenTheSnapshotIsValid` (T6) |
| Stale/deleted snapshot never changes output | `TestStaleOrDeletedSnapshotNeverChangesListOutput` (T6) |
| Prune honours limits, dry-run, never locked/corrupt | `TestPruneHonoursMaxRunsMaxAgeAndMaxTotalBytes`, `TestPruneDryRun...`, `TestPruneNeverRemovesALockedCorruptOrUnsupportedRun` (T7) |
| fsync failure and short write leave readable prefix | `TestInjectedFsyncFailure...`, `TestShortWrite...` (T2) |
| `make check`, race, Windows store tests | every task; Windows via the CI OS matrix |

## Benchmark plan (5,000 runs)

`seedRuns(t, dir, 5000)` writes terminal logs of about 20 events and valid
snapshots directly, without fsync. The gate (`TestListFiveThousandRunsMeetsTheBudget`)
measures a warm second `List` and fails above 1 s on Linux; it is skipped under
`-short` and the race detector, and logs the Windows time without gating.
`BenchmarkList5000` reports ns/op with `b.ReportAllocs`. The same helper runs a
cold-snapshot variant (all snapshots deleted) to record, not gate, the cost of
the fallback. If the gate fails, the contingency is the rebuildable
`<store>/index.json` the spec describes, as a new task before T7.

## Retention prune rules (summary)

Off by default. Candidates are valid run-ID directories with a terminal event,
no live lock, not corrupt, not unsupported. Order oldest first by `started_at`;
apply `max_age_days`, then `max_runs` (all runs count against the budget), then
`max_total_bytes`. A deletion failure is reported, the run skipped, the others
continue, exit status is non-zero. `--dry-run` reports the same plan and deletes
nothing. Never follows symlinks; never touches Git worktrees or refs.

## Windows plan

- `File.Sync` is `FlushFileBuffers`; lifecycle durability is the same call on
  both OSes. Directory fsync does not exist: `syncDir` is a no-op with a
  documented best-effort creation guarantee.
- `golang.org/x/sys/windows`: `LockFileEx`/`UnlockFileEx` on a one-byte range of
  an empty `run.lock`; handles created by Go are non-inheritable, so node
  children do not keep the lock (proved by the orphan in T5).
- `os.Rename` onto an existing file replaces it but may fail while another handle
  is open; only the disposable snapshot uses it.
- Files open in the writer or a reader block delete: prune reports and skips.
- Kill test uses `TerminateProcess` via `Process.Kill`; no signals.
- Linux/WSL is the reference environment; the CI OS matrix runs the store and CLI
  tests on Windows.

## In-flight branches: ordering and rebase points

- **F3.1 stack** (`claude/f3-1-runtime`, `-cli`, `-guard-and-docs`): edits
  `cmd/agentplane/{cli,serve,json,output}.go` and `internal/runtime/scheduler.go`.
  Conflicts to expect: the single-line usage string at `cli.go:60` (F3.1 adds
  `--worktree-root`, `--retain-worktrees`; F3.2 adds `runs prune`), the
  `serveRunOptions`/`starter` block in `serve.go`, the `cliDocument` struct in
  `json.go`, the `Options` struct hunk next to the `Persist` comment in
  `scheduler.go`, and `docs/architecture.md`, `docs/codebase-map.md`, root
  `AGENTS.md`. Plan: **T1-T4 and T6 touch no F3.1 file** except the one comment
  in T1; develop them now. Rebase T5, T7, T8 and T9 onto F3.1 once its CLI branch
  merges (usage string resolved by hand; no refactor of it). F3.1 retention of
  isolated worktrees and `refs/agentplane/*` is a separate concern from run
  pruning, documented in T7/T9.
- **F2.8b** (`claude/f2-8b-plan`): owns event schema 12 (`attempt_diff`). F3.2 has
  no schema work, so there is no number to claim; the schema-fixture test is
  written against `events.SchemaVersion` (T3), so whichever of F2.8b and F3.2
  merges second must add the missing fixture, and the `unsupported` fixture is
  always `SchemaVersion+1`. F2.8b also edits `cmd/agentplane/{cli,serve}.go`
  (`--record-diffs`) and plans a scratch directory "under the store": run pruning
  and `List` only treat valid run-ID directories as runs, so a scratch directory
  beside them is ignored, and F3.2 asks F2.8b Q6 to keep scratch outside run
  directories. It only adds a reducer tolerance test on the desktop side; T8 edits
  `reducer.ts` and `status.tsx` and conflicts only textually.
- **F3.4** (`claude/f3-4-plan`): next free event schema after 12, additive; its
  "needs the durable store" criteria consume F3.2's persist-before-act boundary
  and `interrupted` derivation. No code overlap; F3.4 should land after T1-T4.
- **F3.8/F3.9** (`claude/f3-8-f3-9-plan`): `00XX` ADR collision only; resolved by
  renaming at merge.
- Merge order. Decided 2026-11-17 (lead, delegated by the product owner); the team
  may revisit: F3.1 stack -> F2.8b -> F3.2 -> F3.4 T1-T9 -> F3.8/F3.9 -> F3.4
  T10-T13. Event schemas (proposed, confirmed against `events.SchemaVersion` at
  merge): F2.8b = 12, F3.4 = 13, F3.8/F3.9 = 14. F3.2 and F3.4 may proceed in
  parallel (no file overlap). T1-T4 and T6 may merge earlier because they do not
  depend on either.

## Risks and edge cases

- **Reader sees a run between `Mkdir` and the first event.** Handled: locked with
  no valid first line is "starting", not `corrupt`; an unlocked one is `corrupt`.
- **Lock on network filesystems** is advisory and unreliable; unsupported
  locations are documented, not detected.
- **Cross-process `running` for streams.** The stream endpoint for a store-only
  run replays then sends `end`; T8 makes the desktop use the store status so it
  does not label such a run `interrupted`.
- **`Collector` lock hold time.** Persist already runs under `c.mu`; T1 adds no
  I/O under the lock.
- **Snapshot trust.** `log_bytes` equality plus terminal status plus run ID; a
  truncated-then-regrown log of equal size cannot occur because terminal logs are
  immutable.
- **Prune races a starting run.** A new writer cannot reuse an existing run ID
  (exclusive `Mkdir`), and prune skips anything locked or unterminated.
- **Existing CLI behavior after a persist error** keeps the run going (A1); only
  delivery changes.

## Ambiguities in the spec (defaults used; owner confirms)

1. Spec says a sink error "stops the run"; only `serve` cancels, CLI `run` keeps
   executing and exits 1. Default: unchanged, wording amended.
2. After a persist error the CLI `--watch` end-of-run replay still prints
   in-memory final facts that are not durable. Default: allowed, because the same
   invocation reports the persistence failure; spec states the exception.
3. Snapshot at `run_started` is never trusted; default: terminal only.
4. "Go reducer" does not exist; default: event-sequence equality plus the desktop
   reducer on a golden log.
5. No command writes `retention`; default: hand-edit `store.json`, validated at
   read; `max_total_bytes` counts run directories only.
6. `store.json` lazily created by concurrent first writers; default: temp file,
   sync, rename, tolerate existing.

## Definition of done for F3.2

Every acceptance criterion in the spec has a named passing test above;
`make check`, `go test -race ./...` and `cd desktop && npm run check` pass; the
Windows store and CLI tests pass in the OS matrix; the architecture baseline is
unchanged; the ADR has its final number; the event schema is unchanged.
