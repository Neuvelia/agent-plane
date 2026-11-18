# F3.4 Cancellation and Timeouts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One process-tree termination contract for every child process the
runtime owns (Shell, Codex, command Gate, Git helpers), opt-in node and run time
limits, typed cancel and timeout reasons in the trace, and Windows parity through
Job Objects (F3.4a), with the Windows CI job becoming blocking.

**Architecture:** A small neutral package `internal/proctree` owns "start a
child, own its tree, terminate it by the contract, report how it ended". Unix uses
a process group, Windows a Job Object, behind one API and `_unix`/`_windows`
files. `internal/runtime` stays free of OS imports: it owns the cause model
(`context.WithCancelCause` with a typed cause defined in `internal/domain/run`),
the deadlines (through an injected `Timers` port, so tests use a fake clock) and
the events. Executors report how their process ended through their error, and
the coordinator turns that into `process_terminated` in a deterministic order.

**Tech Stack:** Go (standard library on Unix; `golang.org/x/sys/windows` becomes
a direct dependency, Windows build only), existing event/runtime/CLI packages.
`golang.org/x/sys v0.28.0` is already in `go.mod` as an indirect requirement, so
T10 only moves it to the direct block (`go mod tidy`, no new module download, no
new supply-chain surface). Dependency-policy justification: Job Objects and
`CREATE_SUSPENDED` assignment have no standard-library API (`syscall` lacks
`CreateJobObject`, `AssignProcessToJobObject` and thread resume); the dependency
is build-tagged to Windows, runtime-only, and maintained by the Go project.

**Spec:** `docs/features/F3.4-cancellation-and-timeouts.md` and
`docs/features/F3.4a-windows-process-tree-termination.md`.
**ADR:** `docs/adr/0028-cancel-timeout-reasons-and-process-ownership.md`
(proposed; number assigned by the owner).
**Evidence:** `experiments/spikes/2026-11-17-landlock-namespaces-jobobject.md`
(branch `claude/spikes-landlock-jobobject`).

Product name: Tarmind (in code today: agentplane). Identifiers, paths and event
names below keep the code name.

## Global Constraints

- Implement only F3.4 and F3.4a. No resume or orphan recovery (F3.3), no durable
  store work (F3.2), no sandbox or quotas (F3.9), no budgets (F3.10), no HTTP
  cancel endpoint (F4.6), no Human node timeout.
- Graphs and runs without limits keep byte-identical behavior: same semantic
  hashes, event sequences and scheduling, apart from the additive fields of the
  new schema on events that now carry a `reason`. Existing tests pass unmodified
  unless a task states why one changes.
- Termination is decided by our own record (the cause and the request we made),
  never by exit code, wait status or error text.
- Termination only signals a tree this process created: the Unix group id or the
  Windows job handle. No PID lookup, no name matching.
- Events carry enums, counts and durations only: no paths, environment or
  command lines.
- No sleeps as synchronization. Process tests use a pipe or ready file written by
  the child. Deadlines are tested with the injected fake `Timers`; real process
  tests use short real grace values only as a bound.
- `internal/runtime` must not import `os/exec`, `syscall` or `x/sys`. The
  architecture baseline must not grow (`internal/archtest/baseline.txt`).
- Every task ends with `make check`, `go test -race` for the touched packages and
  a review of `git diff --stat`. One branch and one PR per task, in order, each
  stacked on the previous. Never merge without an explicit request.
- Keep a local `TASKS.md` journal (Git-ignored), per `AGENTS.md`.

## Review Focus

- Pid reuse and signalling a process we do not own (Unix group lifetime, Windows
  job handle only).
- Classification by cause: a node deadline must not be reported as a plain
  failure, and a run cancel must never be reported as a node timeout.
- Event order: `cancel_requested` < `process_terminated` < node terminal <
  `run_finished`, exactly one `run_finished`, monotonic sequence.
- An executor that ignores its context cannot hold the scheduler, and cannot
  append events after `run_finished` (`appendEvent` panics on a finished
  collector).
- The loop budget: a timed-out attempt consumes exactly one attempt and never
  more than `max_attempts` run (AGENTS.md rule 7).
- Accepted sibling output is untouched by any timeout or cancel (rule 6).
- Windows assignment race (W2) and nested jobs (W7), `CTRL_BREAK` with no console
  (W5).

## Task order and dependencies

```text
Phase A  Unix tree-termination contract
  T1 proctree (Unix) ──> T2 Shell ──> T3 Codex ──> T4 Gate + Git helpers
Phase B  Reasons and timeouts
  T5 events/domain ──> T6 graph limits ──> T7 runtime ──> T8 CLI/serve ──> T9 built-CLI proof
Phase C  Windows (F3.4a)
  T10 proctree (Windows) ──> T11 adopters on Windows + Gate compare ──> T12 CI flip ──> T13 closure
```

T5 and T6 do not depend on Phase A and can be built in parallel with it. T7
needs T2 to T5. Phase C starts only after Phase B is merged: it reuses the
reason and event contract. T1 ships an interim Windows file so the module builds
and W3 (idempotent cancel) is already green there; the Job Object arrives in T10.

---

## Current state (verified in the code)

| Concern | Where | What exists |
| --- | --- | --- |
| Shell process | `internal/shell/executor.go` `execute` | `exec.CommandContext`, `Cancel` = `Process.Kill`, `WaitDelay` 1 s, `killRequested`, `runtime.GOOS == "windows"` branch, no group |
| Codex process | `internal/agent/codex/process_unix.go` `runProcess` | `Setpgid`, SIGTERM, fixed 500 ms, SIGKILL, reap within 1 s, typed `shutdown`; `process_other.go` rejects other OSes; `adapter.go:75` guards `Detect` by `runtime.GOOS` |
| Codex callers | `adapter.go` `Detect` probe (10 s) and `Execute` | `Execute` classifies `ctx.Err() != nil` as `cancelled` |
| Command Gate | `internal/gate/command.go` `command`, `runDiff` | `/usr/bin/diff /dev/fd/3 /dev/fd/4`, `ExtraFiles`, no group, 1 s `WaitDelay`, no overall bound; `config.go` pins `diffCommand` |
| Git helpers | `internal/gitstate/run.go` `Run`, `internal/runtime/workspace.go:45`, `internal/capability/capability.go:244` | `exec.CommandContext` with timeouts, direct child only |
| Scheduler cancel | `internal/runtime/scheduler.go` `Run`, `batch.go`, `accept.go`, `loop.go cycle` | decisions read `ctx.Err()` of the single run context; accept treats `ctx.Err()!=nil` plus `errors.Is(Canceled/DeadlineExceeded)` as cancel; `cycle` runs producer then embedded Gate on the same ctx |
| Retry-pending cancel | `Run`, `if failed \|\| ctx.Err() != nil` branch | nodes left `running` for another attempt become `cancelled` when another node failed or the run was cancelled |
| Events | `internal/domain/events/event.go` (schema 11) | `NodeFinishedPayload`, `NodeTerminatedPayload`, `RunFinishedPayload` carry `status`, `failure_kind`; `attempt.go` outcomes `execution_error`, `cancelled` |
| Event emission | `scheduler.go record()` | builds `node_*` and `run_finished` payloads from transitions |
| Appending | `scheduler.go appendEvent` | panics when the collector rejects an event (including after `run_finished`) |
| CLI | `cmd/agentplane/main.go`, `cli.go` | `signal.NotifyContext(os.Interrupt, SIGTERM)`; `cancelled` -> `exitInterrupted` 130; `serve.go:161` `context.WithCancel` per run |
| Composition | `cmd/agentplane/adapters.go` `nodeExecutor`; `internal/adapter/system` | executors are zero-value structs composed at the root; `system.Clock`, `system.IDs` |
| Architecture test | `internal/archtest/archtest_test.go` | `internal/runtime` is `layerApp` (no `os`, `os/exec`); adapters may not import adapters; `layerNeutral` packages are importable by every layer; baseline may only shrink |
| Tests | `internal/shell/executor_test.go`, `internal/agent/codex/process_test.go` | re-exec helper `TestHelperProcess` in shell; Codex tests use `/bin/sh` fake scripts and `//go:build darwin \|\| linux` |

Two facts shape the design. First, the runtime has no `Timers` or fake clock
today (`trace.Clock` has only `Now`), so deadlines need a small injected port.
Second, `acceptBatchResults` treats an executor error as cancel only when the
**run** context is done; a node deadline cancels a derived context, so without
new classification it would become a plain `FailureExecutor` and, in a loop,
would end the node instead of consuming one attempt.

## Placement decisions

1. **`internal/proctree`, classified `layerNeutral` in `archtest_test.go`**
   (like `internal/fields`). Shell, Codex, Gate, Git runner and capability are
   all adapters and the architecture test forbids adapter-to-adapter imports, so
   a shared adapter package would add baseline violations. A neutral package
   that depends only on the standard library and `x/sys/windows` adds none and is
   importable by `internal/runtime` if it ever needs a type. It must not import
   any project package.
2. **Cause model in `internal/domain/run`** (`CancelReason`, `CancelCause`,
   `ReasonOf(error)`), so adapters can read `context.Cause(ctx)` without
   importing `internal/runtime`.
3. **Executor-to-runtime termination report** through the returned error: the
   error implements `ProcessTermination() events.ProcessTerminatedPayload`; the
   runtime finds it with `errors.As` against a consumer-owned interface in
   `internal/runtime`. This avoids widening `ExecutionObservation` and keeps
   emission on the coordinator.
4. **Policy injection:** `proctree.Policy{Grace time.Duration; Escalate
   <-chan struct{}}` is a field on `shell.Executor`, `codex.Adapter` and
   `gate.Executor`, set at the composition root. The zero value means the default
   grace (5 s) and no second-interrupt channel, so existing literals
   (`shell.Executor{}`) keep compiling and behaving.
5. **`proctree.Start` must not clobber a caller-prepared `SysProcAttr`.** It
   merges `Setpgid` into whatever is set. Decided 2026-11-17 (lead, delegated by
   the product owner); the team may revisit: F3.9 launches processes on top of
   proctree unconditionally, so proctree exposes a `Prepare(cmd)` hook that it
   calls before `Start`. F3.9 uses it for `Cloneflags` and the re-exec helper
   wrapper; proctree's own fields (`Setpgid` on Unix, `CreationFlags` on Windows)
   are set after the hook and merged, never overwritten, and the hook must not
   overwrite fields proctree set. `TestStartKeepsCallerSysProcAttr` and a Prepare
   composition test guard it.

## Contracts (indicative)

```go
// internal/proctree
type Policy struct {
    Grace    time.Duration   // zero selects 5s
    Escalate <-chan struct{} // closed on a second interrupt: skip the rest of grace
}
type Method string // "graceful" | "forced" | "already_exited"
type Outcome struct {
    Method   Method
    Grace    time.Duration // configured
    Elapsed  time.Duration // request to verified empty
    Leaked   int           // descendants still alive after force
}
type Tree struct{ /* *exec.Cmd, group id (Unix) or job handle (Windows) */ }
func Start(cmd *exec.Cmd) (*Tree, error)                 // starts cmd, owns its tree
func (t *Tree) Wait() error                              // cmd.Wait semantics
func (t *Tree) Terminate(ctx context.Context, p Policy) (Outcome, error) // idempotent
type LeakError struct{ Leaked int }                      // typed "shutdown"

// internal/domain/run
type CancelReason string // interrupt | user_cancel | run_timeout | sibling_failed | node_timeout
type CancelCause struct{ Reason CancelReason; Node string }
func ReasonOf(cause error) (CancelReason, bool)

// internal/runtime
type Timers interface{ AfterFunc(d time.Duration, f func()) (stop func() bool) }
// Options gains: Timers Timers; RunTimeout time.Duration (may only shorten the
// graph limit); CancelBound time.Duration (zero: grace default + 1s verify + 1s)
```

`Terminate` receives a context only so an overall bound can stop a wedged
wait. It does not use it to decide the reason. `Terminate` on a `Tree` that
already exited returns `Outcome{Method: already_exited}` and a nil error. On
Unix `Tree` signals `-pgid` only while the group is non-empty, which is also the
pid-reuse guard: a group id cannot be reused while any member is alive, so a
`kill(-pgid, 0)` that reports an empty group is final and nothing is signalled.
The residual window (group empties, id is recycled and becomes a new group
leader between the check and the signal) is microseconds wide and is recorded in
the ADR.

## Event contract (schema 13, proposed)

Decided 2026-11-17 (lead, delegated by the product owner); the team may revisit:
event schema order is F2.8b = 12, F3.4 = 13, F3.8/F3.9 = 14. This plan uses
**13 (proposed; next free number confirmed at merge)**. Implement against a named
constant in `internal/domain/events` (`schemaCancelTimeout`) and confirm it
against `events.SchemaVersion` when T5 is cut. If the order changes, the renumber
is one isolated commit (the constant, fixtures, `docs/architecture.md`, ADR
text). No test hard-codes the literal; they use `events.SchemaVersion` and
`schemaCancelTimeout`.

Merge order (same decision): F3.1 stack -> F2.8b -> F3.2 -> F3.4 T1-T9 ->
F3.8/F3.9 -> F3.4 T10-T13 (Windows Job Objects and making windows-x64 blocking).
F3.2 and F3.4 may proceed in parallel (no file overlap).

| Change | Shape |
| --- | --- |
| `node_finished`, `node_terminated`, `run_finished` | optional `reason` (closed set above) |
| `failure_kind` | adds `timeout`, `shutdown` |
| `attempt_finished.outcome` | adds `timeout` |
| `cancel_requested` | `scope` (`run`\|`node`), `reason`, `source` (`signal`\|`api`\|`deadline`\|`failure`) |
| `process_terminated` | `method`, `grace_ms`, `elapsed_ms`, `leaked` (count) |

Both new types are node-correlated when `scope` is `node` or the event is a
process fact, run-scoped otherwise. Gated in `Event.Validate` on the schema
constant, like `workspace_*` and `value_recorded`.

---

# Phase A: Unix tree-termination contract

### Task 1: `internal/proctree` (Unix) and the process helper

**Branch:** `claude/f3-4-proctree`

**Files:** Create `internal/proctree/{doc,proctree,policy}.go`,
`proctree_unix.go`, `proctree_windows.go` (interim, see below),
`count_linux.go`, `count_other.go`, `internal/proctree/AGENTS.md`,
`internal/proctree/helper_test.go` (re-exec helper), `proctree_test.go`,
`proctree_unix_test.go`. Edit `internal/archtest/archtest_test.go`
(`legacyLayers`: `internal/proctree` as `layerNeutral` with the reason),
`docs/codebase-map.md`.

**Behavior:**

- `Start`: merge `Setpgid: true` into the command's `SysProcAttr`, start, record
  the group id (= leader pid). A start error returns `*exec.Error` unchanged so
  callers keep `FailureStart` classification.
- `Terminate` on Unix: if the leader is reaped and `kill(-pgid, 0)` reports ESRCH
  -> `already_exited`. Otherwise `SIGTERM` to `-pgid`; wait up to `Grace` for the
  leader to exit **and** the group to empty (poll `kill(-pgid, 0)` driven by the
  `Wait` result and a ticker, no sleeps as synchronization of tests); `Escalate`
  closing skips the remainder; then `SIGKILL` to `-pgid` (`forced`); verify empty
  within 1 s; a non-empty group yields `LeakError{Leaked: n}` and `Outcome.Leaked`.
- `Leaked` is exact on Linux (scan `/proc/*/stat` for the group id,
  `count_linux.go`) and a lower bound of 1 elsewhere (`count_other.go`). The ADR
  records this; the event field is documented as `>= 1` when non-zero.
- Idempotent: second `Terminate`, `Terminate` after `Wait`, and `Terminate` on a
  nil `Tree` return success.
- Interim `proctree_windows.go`: no Job Object yet. `Start` starts the command,
  `Terminate` kills the direct process and treats "already finished" as
  `already_exited`. It exists so the module builds and Shell W3 is green on
  Windows from T2 on; W1 stays red there until T10.

- [ ] Write the failing tests first (below). `main` has no package, so "red" is a
  compile failure plus, for the tree guarantee, the Shell test in T2.
- [ ] Implement Unix, the interim Windows file and the Linux counter.
- [ ] Register the neutral layer in the architecture test and confirm the
  baseline file is unchanged (`go test ./internal/archtest`).

**Tests (real children; the helper is the test binary re-executed with a mode
argument, the pattern `internal/shell/executor_test.go` `TestHelperProcess`
already uses; no `/bin/sh`, no Unix tools, so the same tests run on Windows in
T10):**

`TestTerminateKillsGrandchild` (helper `spawn` starts a `hold` grandchild and
writes both pids to an inherited pipe before blocking; after `Terminate` both
processes are gone; pid liveness is checked with `kill(pid, 0)` on Unix),
`TestTerminateGracefulWithinGrace` (helper `exit-on-term`; `Method == graceful`,
`Elapsed < Grace`), `TestTerminateForcedAfterGrace` (helper `ignore-term`;
`Method == forced`, `Elapsed >= Grace`), `TestTerminateAlreadyExitedIsSuccess`,
`TestTerminateBeforeWaitAndAfterWaitIsIdempotent`,
`TestTerminateNeverStartedTreeIsSuccess` (nil receiver and failed `Start`),
`TestEscalateSkipsRemainingGrace` (grace 30 s, `Escalate` closed, finishes under
the verify bound), `TestLeakedDescendantYieldsLeakError` (the injected `kill` drops
SIGTERM and SIGKILL, since a member SIGKILL cannot remove is not constructible;
`errors.As(*LeakError)`, `Leaked >= 1`), `TestEscapedSessionIsOutOfScopeAndNotSignalled`
(helper `setsid-child` leaves the group; `Terminate` succeeds and the escapee
survives, killed by test cleanup), `TestReapedLeaderWithEmptyGroupSendsNoSignal`
and `TestReapedLeaderWhosePidWasReusedSendsNoSignal` (injected `kill`),
`TestSignalsOnlyTheOwnedGroup` (a sibling process in a different group survives
every termination), `TestStartKeepsCallerSysProcAttr` (a field the caller set, for example `Setsid` on Linux, survives the merge with `Setpgid`; guarded by build tag), `TestWaitDrainIsBoundedByWaitDelay` (a descendant holding the
stdout pipe; `Wait` returns within `WaitDelay`), `TestLinuxCountsGroupMembers`.
Run `go test -race ./internal/proctree -count=20`.

---

### Task 2: Shell adopts the contract

**Branch:** `claude/f3-4-shell` (depends on T1)

**Files:** Edit `internal/shell/executor.go`, `executor_test.go`,
`internal/shell/AGENTS.md`, `docs/features/F0.5-shell-node.md` (cancellation
paragraph), `docs/development-workflow.md` (remove the "TerminateProcess: Access
is denied" bullet only after T12; here just note it is fixed in code).

**Seams (verified):** `execute` builds `exec.CommandContext` and sets
`cmd.Cancel`; the `killRequested` flag plus the `runtime.GOOS == "windows"` branch
classify cancel. Replace this:

- Build `exec.Command` (no `CommandContext`), keep `WaitDelay = time.Second`
  (rule 8), `proctree.Start(cmd)`.
- A goroutine owned by `execute` waits on `ctx.Done()` or process end and calls
  `Terminate(ctx-free bound, e.Policy)`. It is joined before `execute` returns, so
  its outcome is read without a race.
- Classification uses the record: `FailureCanceled` iff termination was requested
  by a done context and `Terminate` succeeded (any exit status, any OS exit code);
  `FailureTimeout` iff `run.ReasonOf(context.Cause(ctx))` is `node_timeout`;
  `FailureShutdown` iff `*proctree.LeakError`; otherwise existing kinds. The Unix
  `Signaled()` heuristic and the Windows special case are deleted.
- `ExecutionError` gains `Termination *proctree.Outcome` and the method
  `ProcessTermination()` (design decision 3). `FailureKind` gains `timeout` and
  `shutdown`.
- `shell.Executor` gains an optional `Policy proctree.Policy` field; the zero
  value keeps every existing call site valid.

- [ ] Red: add the tree and idempotence tests, watch `TestExecuteCancellationKillsGrandchild`
  fail on `main` behavior (grandchild survives).
- [ ] Port `execute`; keep existing shell tests unmodified.
- [ ] Delete the Windows branch and the `runtime` import.

**Tests:** `TestExecuteCancellationKillsGrandchild` (red today; helper `spawn`),
`TestExecuteNodeDeadlineKillsTreeAndReportsTimeout` (a derived context with a
typed `node_timeout` cause; `FailureTimeout`, no descendant),
`TestExecuteCancelAfterExitIsNotAnExitFailure` (helper exits as cancel fires;
`FailureCanceled` or success, never `FailureExit`; this is W3 on Windows),
`TestExecuteForcedKillRecordsMethod` (helper `ignore-term` with a 200 ms policy
grace; `Termination.Method == forced`), `TestExecuteLeakIsShutdownWithCount`,
`TestExecuteExitCodeZeroAfterKillStillCanceled` (helper that traps TERM and exits
0; classification from the record, not the code), existing
`TestExecuteCancellationTerminatesChild` and
`TestExecuteCancellationReturnsWithInheritedOutputPipes` unchanged.

---

### Task 3: Codex uses the shared helper

**Branch:** `claude/f3-4-codex` (depends on T1; independent of T2)

**Files:** Edit `internal/agent/codex/{adapter.go,process_unix.go}`,
`internal/agent/errors.go`, `internal/agent/AGENTS.md`,
`internal/agent/codex/AGENTS.md`, `process_test.go`. Rename
`process_unix.go` to `process.go` (no build tag) only in T11; here keep the file
and its tag so the macOS/Linux diff is a pure refactor.

**Behavior:** `runProcess(ctx, binary, args, env, dir, stdin, stdout)` gains the
policy parameter (an unexported struct field on `Adapter`, `Adapter.Termination
proctree.Policy`) and delegates ownership and termination to `proctree`. The
existing observable behavior is preserved: group per invocation including probes,
leader success with an inherited pipe returns nil within `WaitDelay`, typed
`shutdown`/`cancelled`. New: the default grace is the shared 5 s instead of the
fixed 500 ms (`Adapter.Termination.Grace` can pin it for tests), `Outcome` and
`LeakError` flow into the typed `agent.ExecutionError` so the runtime can emit
`process_terminated`, and `Execute` maps `run.ReasonOf(context.Cause(ctx)) ==
node_timeout` to `codex_timeout` (new code in the `fail` allow-list at
`adapter.go:39`, new `agent.ErrorTimeout`) before the generic `cancelled`
mapping at `adapter.go:210`.

- [ ] Red: `TestExecuteNodeDeadlineIsCodexTimeout` and the grace test, plus the
  runtime-visible `ProcessTermination()` on a cancelled execution.
- [ ] Refactor `runProcess` onto `proctree`; run the unmodified existing tests
  (`TestOwnedChildCancellationAndOrdinaryExit`,
  `TestOwnedChildReapedBetweenTerminationObservations`,
  `TestInitiallyIgnoredSpecialEntryIsNotRead`,
  `TestOwnedChildTerminationObservationFailsClosedForLiveProcess`).

**Tests:** the four existing process tests unchanged;
`TestCancelledExecutionCarriesGracefulTermination`,
`TestDeadlineCauseMapsToCodexTimeoutNotCancelled`,
`TestCodexForcedKillAfterGraceWithDescendant` (fake CLI with a TERM-ignoring
descendant), `TestSharedGraceDefaultIsFiveSeconds` (policy value only, no wait).

---

### Task 4: Command Gate and Git helpers

**Branch:** `claude/f3-4-gate-git` (depends on T1)

**Files:** Edit `internal/gate/command.go`, `internal/gate/executor_test.go` or a
new `command_timeout_test.go`, `internal/gate/AGENTS.md`,
`internal/gitstate/run.go`, `internal/gitstate/AGENTS.md`,
`internal/runtime/workspace.go`, `internal/capability/capability.go`,
tests next to each.

**Behavior:**

- Gate: the `/usr/bin/diff` child starts through `proctree.Start`; a fixed
  `commandEvidenceBound = 30 * time.Second` context wraps the whole evidence
  step. Exceeding it returns a typed operational error with failure kind
  `timeout` (never a `red` verdict: a timeout is not evidence about the candidate,
  AGENTS.md rule 5). `runDiff` keeps its separation of lifecycle failures from the
  two valid comparison exits. The bound is independent of the node limit and is
  injectable in tests (`gate.Executor.EvidenceBound`, zero means 30 s).
- Git helpers: `gitstate.Run`, the `git rev-parse` in `runtime/workspace.go`, and
  the capability probe start through `proctree.Start` and terminate by the
  contract, so a Git child that spawns a helper cannot outlive its time bound.
  `gitstate.Run`'s `RunError` classes are unchanged. `internal/runtime/workspace.go`
  keeps its existing baseline line (still imports `os/exec`); no new violation.

- [ ] Red: `TestCommandGateTimeoutIsNotARedVerdict` against a fake diff that blocks
  (the fake is injected through an unexported command path variable used only by
  tests, never from graph config; production keeps the fixed
  `/usr/bin/diff`).
- [ ] Red: `TestGitRunTimeoutKillsGrandchild` using a fake `git` that spawns a
  child (a test `PATH` is not used: `gitstate.Run` is hardened and absolute; add
  an unexported `gitBinary` seam for tests).
- [ ] Port the three callers.

**Tests:** `TestCommandGateTimeoutIsNotARedVerdict`,
`TestCommandGateKillsDiffTreeOnCancel`,
`TestCommandGateGreenRedUnchanged` (existing tests untouched),
`TestGitRunTimeoutKillsGrandchild`, `TestGitRunCancelClassesUnchanged`,
`TestCapabilityProbeBoundedTree`.

**Phase A exit:** on Linux and macOS a cancelled or timed-out Shell, Codex, Gate
or Git child leaves no descendant; Windows still builds on the interim file.

---

# Phase B: reasons and timeouts

### Task 5: Domain types and event schema

**Branch:** `claude/f3-4-events` (independent of Phase A)

**Files:** Create `internal/domain/run/cause.go`, `cause_test.go`,
`internal/domain/events/cancel.go`, `cancel_test.go`. Edit
`internal/domain/events/event.go` (schema constant, `Validate`, payload structs
with `reason`, payload switch at the `payloadType` mapping, clone),
`attempt.go` (outcome `timeout`), `internal/domain/run/result.go` (`FailureTimeout`,
`FailureShutdown`, `Result.Reason`, `NodeResult.Reason`),
`internal/domain/events/AGENTS.md`, `docs/architecture.md` (event list),
`docs/domain-model.md`, `desktop/test` (one reducer test).

**Contract:** the table in "Event contract". Strict validation: closed enums;
`cancel_requested` requires `scope`/`reason`/`source`, node correlation when scope
is `node`; `process_terminated` requires node correlation and bounded integers
(`leaked >= 0`, durations `>= 0`); `reason` only valid on a cancelled or
timeout-failed terminal; new types and fields rejected below the new schema;
payloads detached on clone.

- [ ] **Schema number.** 13 (proposed; next free number confirmed at merge).
  Check `origin/main` for the highest `SchemaVersion` (expected 12 from F2.8b);
  amend the ADR and spec text in the same PR if it differs.
- [ ] Failing tests first, then the types.
- [ ] Verify `desktop/src/reducer.ts` ignores unknown types and tolerates the new
  optional fields; add a reducer test with `cancel_requested` and
  `process_terminated`.

**Tests:** `TestCancelEventsRoundTripAndBounds`,
`TestCancelEventsRequireNewSchema`, `TestReasonOnlyOnCancelledOrTimeoutTerminals`,
`TestReasonSetIsClosed`, `TestCancelEventsCloneIsDetached`,
`TestOlderSchemaFixturesStillDecode` (schemas 1 to current, including a schema 11
stream), `TestReasonOfFindsTypedCauseOnly` (an `errors.New("run_timeout")` text or
a wrapped message never yields a reason), store round-trip in `internal/store`,
desktop reducer test.

---

### Task 6: Graph limits surface (`timeout_seconds`, `limits.run_timeout_seconds`)

**Branch:** `claude/f3-4-limits` (independent of Phase A; after T5 only for the
doc text)

**Files:** Edit `internal/domain/graph/definition.go` (`NodeDefinition.TimeoutSeconds
*int`, `Definition.Limits`), `validation.go` (`E_TIMEOUT_INVALID`),
`internal/adapter/yamlgraph/{dto.go,yamlgraph.go}` (strict DTO fields),
`internal/domain/graph/AGENTS.md`, `docs/graph-spec.md`,
`docs/domain-model.md`. Tests in `internal/domain/graph`, `internal/adapter/yamlgraph`
(`validation_test.go`, `version_test.go`), `internal/adapter/yamlgraph/edit`.

**Contract:** both fields optional, integer 1..86400, semantic (hashed), absent
means none; `E_TIMEOUT_INVALID` carries node, path (`nodes.<id>.timeout_seconds`
or `limits.run_timeout_seconds`) and a suggestion; set on a `human` node is
rejected. Schema version unchanged. YAML decoding of `1.5` or `"30"` must yield
`E_TIMEOUT_INVALID` with the path, not a generic decode error (decide in the first
red test how `yaml.v3` reports a float into `*int` and map it in the adapter).

- [ ] Failing tests first (validation table, hash, YAML round trip).
- [ ] Confirm the line-based editor (`internal/adapter/yamlgraph/edit`) and the
  API graph DTOs (`internal/api/docs_dto.go`, `inspector_dto.go`) preserve both new
  fields on save: add a round-trip test with a graph that uses them. Graph-edit
  operations for the fields are a later slice (spec).

**Tests:** `TestTimeoutSecondsValidationTable` (0, -1, 86401, 1.5, string, human
node, absent), `TestRunLimitValidationTable`, `TestLimitsChangeSemanticVersion`,
`TestLayoutMetadataStillDoesNotChangeVersion`,
`TestYAMLRoundTripKeepsLimits`, `TestEditorSavePreservesLimits`, a parser-level
`TestE_TIMEOUT_INVALID_ReportsPath`.

---

### Task 7: Runtime: causes, deadlines, classification, abandon bound

**Branch:** `claude/f3-4-runtime` (depends on T2 to T6; see Rebase points)

This is the largest task. Land it as ordered commits, each green.

**Files:** Edit `internal/runtime/{scheduler,batch,accept,loop,preflight,human,unit}.go`
as noted, create `internal/runtime/cancel.go` (cause helpers, `Timers`,
`ensureCancelRecorded`, abandon bound), `internal/runtime/cancel_test.go` and
`cancel_timeout_test.go`, `internal/adapter/system/system.go` (`Timers`),
`internal/runtime/AGENTS.md`, `docs/architecture.md`, `docs/graph-spec.md`,
`docs/features/F0.4-dag-scheduler.md` (cancellation paragraph).

**Seams (verified):**

- Single run context drives every decision. Run derives
  `runCtx, cancel := context.WithCancelCause(ctx)` right after preflight. Every
  internal canceller goes through `m.cancelRun(cause)`, which first calls
  `ensureCancelRecorded`, then `cancel(cause)`.
- `cancel_requested` is emitted from the coordinator, exactly once
  (`sync.Once` inside `ensureCancelRecorded`), at the latest at the top of
  `acceptBatchResults` and at the top of each `Run` loop iteration, and promptly by
  a watcher (`context.AfterFunc(runCtx, m.ensureCancelRecorded)`) so live delivery
  shows the cancel while workers drain. A `WaitGroup` joins the watcher before
  `finish`, so nothing appends after `run_finished`. This guarantees
  `cancel_requested` precedes any `process_terminated`, which is only emitted by
  the coordinator when it consumes a worker result.
- Reason of an external parent cancel: `run.ReasonOf(context.Cause(parent))`;
  an untyped `context.Canceled` means `interrupt` (the CLI default). `serve.go`
  supplies typed causes (T8). The `source` is derived from the reason, not
  supplied by callers.
- Node deadline: in `cycle()` (`loop.go`) wrap only the **producer** invoke:
  `nodeCtx, cancelNode := context.WithCancelCause(ctx)`, timer started when the
  attempt begins executing (not at queue time, not around the embedded Gate), fired
  through `Options.Timers.AfterFunc` with `run.CancelCause{Reason: node_timeout}`.
  Non-loop nodes get the same wrapper around their single `invoke`. Human nodes
  never get one (`humanNode` is unchanged; the run deadline still ends them).
  `cycle` decides the typed outcome: if the executor error is a context error,
  `ctx.Err() == nil` and `context.Cause(nodeCtx)` is `node_timeout`, the
  `completedNode.err` becomes a typed `nodeTimeoutError` with
  `DiagnosticCode() == "E_NODE_TIMEOUT"`. If the run context is also done, the run
  cause wins ("first recorded wins": the cause already stored on the machine).
- `acceptBatchResults`: loop branch gets a new outcome `timeout` for a
  `nodeTimeoutError` (no check): `AttemptFinished{Outcome: "timeout"}`; retry when
  `state.numbers[id] < Loop.Attempts()` with `state.feedbacks[id] = nil` (spec Q3:
  no feedback after a timeout) and not retried otherwise; exhaustion ends the node
  `FailureTimeout` (not `FailureNeedsIntervention`). Non-loop: `FailureTimeout`
  before the existing `FailureExecutor` branch. The existing
  `ctx.Err() != nil && errors.Is(...)` cancel branch is unchanged for run-level
  cancel. An unrelated failure keeps precedence (existing `failed` handling).
- `process_terminated` is emitted by the coordinator from the error's
  `ProcessTermination()` just before the `attempt_finished`/terminal transition for
  that node, so the order is `cancel_requested` < `process_terminated` < attempt/
  node terminal < `run_finished`.
- `record()` builds payloads: `node_finished`/`node_terminated` take `Reason` from
  `NodeResult.Reason` (set before `transitionNode`), `run_finished` from the
  machine's run reason. Reasons: run-level cancel -> the stored cause;
  retry-pending nodes cancelled by `if failed || ctx.Err() != nil` ->
  `sibling_failed` when a node failed, the run reason otherwise; unstarted nodes
  via `cancelUnstarted` -> the run reason.
- `finish`: on `RunCancelled` the run reason comes from the machine. If another
  node failed first the run is `failed` (unchanged).
- Run deadline: after `run_started` (in `initializeRun`) start
  `Timers.AfterFunc(limit, func(){ m.cancelRun(CancelCause{run_timeout}) })`, stopped
  in `finish`. Effective limit = min(graph limit, `Options.RunTimeout` when set).
  Preflight: a graph that has any limit, or `RunTimeout > 0`, with `Options.Timers
  == nil` fails with a plain error (programming error, like missing `Clock`).
- Abandon bound: `runBatchWorkers` today blocks on `<-completed`. After the first
  cancel observation (run or node cause) the coordinator arms
  `Timers.AfterFunc(CancelBound, ...)`. Workers that have not returned by then are
  synthesized as `completedNode{err: shutdownError}` (node `failed`,
  `failure_kind: shutdown`), the late result goes into the buffered channel and is
  dropped, and the goroutine is left to finish on its own. The observer wrapper in
  `invoke` checks an `abandoned` atomic under `logMu` so a late log, progress or
  check call can never reach `appendEvent` (which panics after `run_finished`). An
  isolated worktree whose node was abandoned is retained, not removed, because its
  process may still touch it (F3.1 `releaseIsolated`, `workspace_released` reason
  `retained`).

**Commits (in order):**

- [ ] **7a Timers port and cause plumbing.** `Timers` interface, `system.Timers`,
  `Options` fields, `runCtx` with cause, `ensureCancelRecorded`, watcher join,
  `NodeResult.Reason` and run reason in `record()`. No deadlines yet. Reasons
  `interrupt` and `user_cancel` pass through; `sibling_failed` for retry-pending
  cancels.
- [ ] **7b Process facts.** `process_terminated` from error reports, ordering rule.
- [ ] **7c Node deadline.** Producer wrapper, `nodeTimeoutError`, accept branches,
  loop attempt budget, `FailureTimeout`.
- [ ] **7d Run deadline.** Timer after `run_started`, effective-limit rule,
  preflight check.
- [ ] **7e Abandon bound.** Synthesized `shutdown`, observer guard, isolation
  retention.

**Tests (fake executor, fake `Timers`, fake clock, explicit barriers; no sleeps):**

- `TestNodeDeadlineEndsOnlyThatNodeFailedTimeout` (A accepted, B hangs until its
  timer fires; A's outputs and AttemptID unchanged, invocation counts 1; criterion
  "node deadline ends only that node").
- `TestLoopTimeoutConsumesExactlyOneAttempt` (attempt 1 times out, attempt 2
  accepted; attempt events `timeout`, `accepted`; no `Feedback` on attempt 2).
- `TestLoopTimeoutExhaustionIsFailedTimeoutNotIntervention` and
  `TestLoopNeverExceedsMaxAttemptsWithTimeouts` (all attempts time out, exactly
  `max_attempts` invocations).
- `TestRunDeadlineCancelsRunWithReason` (`run_finished` status `cancelled`, reason
  `run_timeout`; running and unstarted nodes `cancelled`; every started node has a
  terminal event; sequence monotonic; exactly one `run_finished`).
- `TestFourCancelReasonsAreDistinct` (interrupt, `user_cancel`, `run_timeout`,
  `sibling_failed`).
- `TestUnrelatedFailureKeepsPrecedenceOverCancel` (F0.4).
- `TestReasonIsDecidedByCauseNotErrorTextOrExitCode` (executor returns an error
  whose message says "run_timeout" and carries a fake exit code; the reason follows
  `context.Cause`).
- `TestIgnoringExecutorIsAbandonedAtBound` (executor blocks on a barrier the test
  never releases until after `Run` returns; node `failed`/`shutdown`; `go test
  -race` clean; no event after `run_finished`).
- `TestCancelEventOrder` (`cancel_requested` < `process_terminated` < node terminal
  < `run_finished` for interrupt, run deadline and node deadline).
- `TestCancelBeforeNodeStartsTerminatesWithReasonAndNoProcess`.
- `TestHumanNodeEndsWithRunDeadline` and `TestHumanNodeRejectsTimeoutAtPreflight`.
- `TestAcceptedSiblingNeverRerunAfterTimeoutOrCancel`.
- `TestIsolatedAbandonedNodeWorktreeIsRetained` (after the F3.1 rebase).
- `TestNoLimitsBytewiseUnchanged` (golden event sequence for an unlimited run
  equals the pre-change fixture apart from schema).
- `go test -race ./internal/runtime/... -count=20` on the concurrency tests.

---

### Task 8: CLI and serve composition

**Branch:** `claude/f3-4-cli` (depends on T7)

**Files:** Edit `cmd/agentplane/{main,cli,serve,adapters,json,output}.go`, tests,
`cmd/agentplane/AGENTS.md`, `docs/features/F0.7-cli-runner.md` (exit code table),
`docs/adr/0007-*.md` (exit codes, cross-reference), `docs/graph-spec.md`.

**Behavior:**

- `main.go`: replace `signal.NotifyContext` with a counted signal handler: the
  first `os.Interrupt` (and `SIGTERM` on Unix) cancels the root context with a
  typed `interrupt` cause; the second closes a `proctree.Policy.Escalate` channel
  passed down by `runCommand`. Windows only reacts to `os.Interrupt`; the
  `syscall.SIGTERM` in the signal list stays harmless (F3.4a).
- `cli.go run`: flags `--cancel-grace DURATION` (default 5 s, 100 ms..5 min) and
  `--run-timeout SECONDS` (integer; sets `Options.RunTimeout`; effective limit is
  the minimum with the graph limit, never an extension). Compose `Policy` into
  `shell.Executor`, `codex.Adapter` and `gate.Executor` in `adapters.go`
  (`nodeExecutor`). Compose `system.Timers{}`. Update the usage string.
- Exit codes: add `exitRunTimeout = 124` for run `cancelled` with reason
  `run_timeout`; `interrupt`/`user_cancel` stay 130. Node deadline ends the run
  `failed`, exit 4. A watch-output failure cancel is `interrupt`.
- `--json`: `reason` on the run and on each cancelled/timeout node, absent
  otherwise; envelope version stays 1 (additive). Human renderer: one line per
  cancelled or timed-out node with its reason and, for forced kills, "forced after
  Ns" from `process_terminated`.
- `serve.go:161`: the per-run `context.WithCancel` becomes `WithCancelCause` with
  `user_cancel`; server shutdown (`serveCtx`) cancels with `interrupt`. There is no
  HTTP cancel endpoint today and none is added here.

- [ ] Red tests first (below), then the flag and exit-code changes.

**Tests:** `TestRunTimeoutExitsWithDedicatedCode`, `TestRunTimeoutFlagCanOnlyShorten`
(flag longer than the graph limit has no effect; shorter wins),
`TestCancelGraceFlagValidation`, `TestJSONReasonPresentOnlyWhenCancelled`,
`TestHumanOutputShowsReasonAndForcedAfter`, `TestServeRunCancelUsesUserCancel`,
`TestServeShutdownUsesInterrupt`, golden JSON for an unlimited run unchanged
(bytes).

---

### Task 9: Built-binary proof

**Branch:** `claude/f3-4-cli-proof` (depends on T8)

**Files:** Create `cmd/agentplane/signal_unix_test.go` (`//go:build !windows`),
`cmd/agentplane/timeout_integration_test.go`; extend `cmd/agentplane/testdata`
graphs (shell nodes that run the test-binary helper). Edit `docs/development-workflow.md`.

**Tests (build the CLI once per test binary, as `integration_test.go` does):**
`TestBuiltCLISIGINTIsInterruptExit130NoLiveChild`,
`TestBuiltCLISIGTERMIsInterruptExit130NoLiveChild` (Unix),
`TestBuiltCLIRunDeadlineExits124WithReasonInJSONAndHuman`,
`TestBuiltCLISecondInterruptSkipsGrace` (grace 30 s; second signal finishes under
the verify bound),
`TestBuiltCLINodeDeadlineIsFailedTimeoutExit4`,
`TestBuiltCLIForcedKillMessage`. Each asserts no live child through the helper's
pid pipe.

**Phase B exit:** all "Can ship before F3.2" criteria except W1 to W8 are
covered on Linux and macOS.

---

# Phase C: Windows (F3.4a)

### Task 10: `internal/proctree` on Windows (Job Objects)

**Branch:** `claude/f3-4a-jobobject` (after Phase B merged)

**Files:** Replace the interim `internal/proctree/proctree_windows.go` with
`proctree_windows.go`, `job_windows.go`, `thread_windows.go`, add
`proctree_windows_test.go`. Edit `go.mod`/`go.sum` (`golang.org/x/sys` moves from
`// indirect` to direct; `go mod tidy`, no new module),
`internal/proctree/AGENTS.md`, ADR text.

**Behavior (from the spike, `2026-11-17-landlock-namespaces-jobobject.md`):**

- `Start`: create a job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, no breakaway.
  Assignment strategy **CREATE_SUSPENDED**: set `SysProcAttr.CreationFlags =
  CREATE_SUSPENDED | CREATE_NEW_PROCESS_GROUP`, `cmd.Start`, assign the process
  handle to the job, resume the primary thread, then return. Go does not expose the
  primary thread, so find it with a Toolhelp thread snapshot of the new PID
  (`thread_windows.go`). The snapshot has a small TOCTOU on a fresh PID; it is
  closed by the fact that `exec.Cmd` keeps the process handle open until `Wait`, so
  the PID cannot be recycled while we look (verify in W2; if it fails, switch to
  `PROC_THREAD_ATTRIBUTE_JOB_LIST` with raw `CreateProcessW`, which has no window,
  and record the change in the ADR). Spike result: both strategies 0 of 200
  escapes; starting normally and assigning later is rejected (50 ms delay escaped
  20 of 20). A failed job creation or assignment fails the node `start` with the
  Win32 error and kills the suspended child.
- `Terminate`: request with `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid)`
  scoped to our process group; with no console (error `The handle is invalid`, the
  spike's detached case) or any error, wait a fixed 500 ms (F3.4a Q3, a `Policy`
  default that tests override), then `TerminateJobObject` with a fixed exit code
  that is never used for classification. Wait for the leader, poll
  `JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.ActiveProcesses == 0` within the 1 s
  verify bound; non-zero -> `LeakError{Leaked: ActiveProcesses}`. Close the job
  handle last. A process killed by job close reports exit code 0 (spike), so the
  runtime's record, not the code, classifies (already the T2 rule).
- Crash safety: `KILL_ON_JOB_CLOSE` kills the tree when the runner dies.
- Nested jobs are a requirement of the supported floor (Windows 10 / Server 2019).

- [ ] Red: the helper tests from T1 run on Windows; W1 and W2 fail on the interim
  file.
- [ ] Implement, then W3 to W7.

**Tests (Windows CI runner, helper built from the test binary, explicit
synchronization, no sleeps):** `TestWindowsTreeKillGrandchild` (W1),
`TestWindowsStartupRaceDescendantsKilled` (W2: child's first action spawns a
grandchild, 200 iterations, handles held from creation to avoid PID reuse),
`TestWindowsCancelExitedChildIsSuccess` (W3), `TestWindowsRunnerCrashClosesJob`
(W4: the test re-execs a `runner` mode that starts a tree, the test terminates the
runner by handle, the grandchild is gone), `TestWindowsNoConsoleForcesAfterWait`
(W5: runner re-exec with `DETACHED_PROCESS`, force within wait plus verify bound),
`TestWindowsNestedJob` (W7: the test re-execs itself assigned to an outer job and
repeats W1 to W3). The shared helper tests (T1) also run here. Skipped on other
OSes by build tag, not by `testenv` guards.

---

### Task 11: Shell, Codex and Gate on Windows

**Branch:** `claude/f3-4a-adopters` (depends on T10)

**Files:**

- Codex: rename `process_unix.go` -> `process.go` (no build tag, now one
  implementation over `proctree`), delete `process_other.go`, remove the
  `runtime.GOOS` guard in `adapter.go` `Detect` (line 75) and the
  `unsupported_platform` allow-list entry only after W6 is green. Port
  `process_test.go` to the helper-binary fake (the existing tests use `/bin/sh`
  scripts and FIFOs, so they stay behind `darwin || linux`; add
  `process_helper_test.go` with the portable subset). `internal/agent/codex/AGENTS.md`,
  `docs/features/F1.2-first-coding-agent-adapter.md` (platform note).
- Shell: no code change expected beyond T2; confirm `executor.go` has no
  `runtime` import left and enable the Windows subset of shell tests.
- Command Gate: split `internal/gate/command.go` into the dispatcher plus
  `command_diff_unix.go` (`//go:build darwin || linux`, today's `runDiff` path,
  unchanged evidence) and `command_compare.go` (portable, compiles everywhere so
  Linux CI tests it). Add `useInProcessCompare` as a build-tagged constant (true on
  Windows). The Windows path takes the two already opened regular files from
  `openCompared` and compares them in fixed 32 KiB blocks, checking the evidence
  context between blocks, read-only, no diff text: green `files match`, red `files
  differ`, evidence `Source: "command"` with no exit code and empty stdout/stderr
  (confirm `events.GateEvidence` validation accepts an absent exit code first; if
  not, use the existing nil-`ExitCode` form already allowed for non-process
  evidence). `config.go` keeps accepting the graph literal `diffCommand`
  (`/usr/bin/diff`) so one graph file stays portable; it is an identifier of the
  comparison, not a path executed on Windows. Remove the `runtime.GOOS` guard at
  `command.go:63`. Check `openCompared` against Windows paths: `EvalSymlinks(cwd) ==
  cwd` can differ on 8.3 short names or case, which is one of the issue #87 items;
  normalize the test fixtures with `filepath.EvalSymlinks(t.TempDir())`.
  `internal/gate/AGENTS.md`, `docs/features/F1.4-gate-node.md`.

**Tests:** `TestWindowsCodexFakeCLINoProcessLeftAfterCancel` (W6, helper-binary
fake, a Windows mirror of `TestOwnedChildCancellationAndOrdinaryExit`),
`TestWindowsCodexSuccessfulLeaderWithInheritedPipeIsBounded` (W6),
`TestCompareFilesGreenRedAndBounds` (portable, runs on Linux: equal, differ by one
byte, differ in length, empty files, large file crossing block boundaries, context
canceled between blocks -> operational error not red),
`TestCompareFilesNeverWritesInputs`, `TestCommandGateWindowsEvidenceHasNoDiffText`
(Windows), `TestCommandGateUnixEvidenceUnchanged` (existing tests, byte-identical),
`TestCommandGateConfigStillRequiresCanonicalAbsoluteCwdOnWindows`. W8, the real
Codex smoke on a Windows host, is manual: record the commands, versions and output
in `docs/validation/F3.4a-windows-real-codex.md` in this PR (not CI).

---

### Task 12: Make `check (windows-x64)` blocking

**Branch:** `claude/f3-4a-ci` (depends on T11 and on the issue #87 fixes)

**Files:** Edit `.github/workflows/ci.yml`, `docs/development-workflow.md`,
`internal/testenv/testenv.go` (guards that no longer have users),
`scripts/check.sh` if the race step needs a Windows variant.

Prerequisites owned elsewhere, required before the flag change and not part of
this plan: issue #87 failures fixed in code or by guards that state a reason (file
modes, 8.3 short paths, verifychanges cases); the `exec.LookPath("git")` change
(own issue) so Codex evidence works on Windows; three consecutive green `main`
Windows runs.

- [ ] Add a Windows step `go test -race ./internal/shell/... ./internal/proctree/...
  ./internal/agent/codex/...` if the runner has a C toolchain; otherwise record the
  accepted alternative from F3.4a Q2 (Linux race coverage plus W1 to W7 without
  `-race` on Windows). Verify on the first run, do not assume.
- [ ] Confirm W1 to W7 run in the job and that no Windows skip remains for process
  lifecycle; list remaining skips with their owning feature in
  `docs/development-workflow.md`.
- [ ] **Own commit:** remove `experimental: true` and `continue-on-error` for the
  Windows entry. A hung test stays capped by `timeout-minutes`.

**Verification:** the PR shows three consecutive green Windows runs on the branch
before the flag commit, and `main` after merge.

---

### Task 13: Closure and documentation

**Branch:** `claude/f3-4-docs`

**Files:** Edit `docs/features/F3.4-*.md`, `F3.4a-*.md` (status, tick only what a
named test proves, leave the D criteria open), `docs/adr/0028-*.md` (Accepted, assigned schema number, the Windows assignment decision, the `Leaked`
lower-bound note, the pid-reuse residual), `docs/adr/README.md`,
`docs/features/README.md`, `docs/roadmap.md`, `docs/architecture.md`,
`docs/domain-model.md`, `docs/graph-spec.md`, `docs/codebase-map.md`, `AGENTS.md`
("Current product phase" paragraph), `docs/development-workflow.md`.

- [ ] Confirm every reference uses the assigned ADR number 0028 (spec headers,
  plan, codebase map).
- [ ] State honestly what is not done: durable criteria (D), Unix crash orphans,
  `setsid` escape, desktop display of the reason, graph-edit operations for the new
  fields, HTTP cancel endpoint.
- [ ] Final review pass: run the `code-review` skill on the combined diff, then
  `make check` and `go test -race ./...`.

---

## Acceptance criteria to test map

| Spec criterion | Test (task) |
| --- | --- |
| U: limits validate, `E_TIMEOUT_INVALID` with path, absent = none | `TestTimeoutSecondsValidationTable`, `TestRunLimitValidationTable` (T6) |
| U: limits change graph version, layout does not | `TestLimitsChangeSemanticVersion`, `TestLayoutMetadataStillDoesNotChangeVersion` (T6) |
| R: node deadline ends only that node, sibling untouched | `TestNodeDeadlineEndsOnlyThatNodeFailedTimeout`, `TestAcceptedSiblingNeverRerunAfterTimeoutOrCancel` (T7) |
| R: loop timeout consumes one attempt, bounded | `TestLoopTimeoutConsumesExactlyOneAttempt`, `TestLoopTimeoutExhaustionIsFailedTimeoutNotIntervention`, `TestLoopNeverExceedsMaxAttemptsWithTimeouts` (T7) |
| R: run deadline, every node terminal, one `run_finished` | `TestRunDeadlineCancelsRunWithReason` (T7) |
| R: four distinct reasons, unrelated failure precedence | `TestFourCancelReasonsAreDistinct`, `TestUnrelatedFailureKeepsPrecedenceOverCancel` (T7) |
| R: reason from cause, not text or exit code | `TestReasonIsDecidedByCauseNotErrorTextOrExitCode`, `TestReasonOfFindsTypedCauseOnly` (T7, T5) |
| R: ignoring executor cannot hold the scheduler | `TestIgnoringExecutorIsAbandonedAtBound` (T7) |
| P: grandchild gone after cancel and deadline (Linux, macOS, Windows) | `TestTerminateKillsGrandchild` (T1), `TestExecuteCancellationKillsGrandchild`, `TestExecuteNodeDeadlineKillsTreeAndReportsTimeout` (T2), `TestWindowsTreeKillGrandchild` (T10) |
| P: graceful vs forced recorded | `TestTerminateGracefulWithinGrace`, `TestTerminateForcedAfterGrace` (T1) |
| P: cancel of exited or never-started is `cancelled`, no error | `TestTerminateAlreadyExitedIsSuccess`, `TestTerminateNeverStartedTreeIsSuccess` (T1), `TestExecuteCancelAfterExitIsNotAnExitFailure` (T2) |
| P: Codex fake CLI with descendant, Gate bounded at 30 s | `TestCodexForcedKillAfterGraceWithDescendant` (T3), `TestCommandGateTimeoutIsNotARedVerdict` (T4) |
| P: leaked descendant is a typed `shutdown` with count | `TestLeakedDescendantYieldsLeakError` (T1), `TestExecuteLeakIsShutdownWithCount` (T2) |
| C: SIGINT/SIGTERM `interrupt`, exit 130, no live child; run deadline code; reason in JSON and human; second interrupt skips grace | `TestBuiltCLI*` (T9), `TestRunTimeoutExitsWithDedicatedCode`, `TestJSONReasonPresentOnlyWhenCancelled` (T8) |
| Events: `cancel_requested` < `process_terminated` < node terminal < `run_finished` | `TestCancelEventOrder` (T7) |
| W1 to W7 | T10 tests named above; W6 in T11 |
| W8 | manual record `docs/validation/F3.4a-windows-real-codex.md` (T11) |
| `go test -race` runtime, shell, codex, helper; `make check`; baseline does not grow | every task; baseline test in T1 |
| D criteria | not in this plan's tasks; see "Durable criteria" |

## Durable criteria (after F3.2, PR #108)

F3.2 is still spec and ADR only on `claude/f3-2-plan`. Do not start these until
its store lands; they are listed so the seams in T5 and T7 do not need rework.

- `cancel_requested` is appended before the context is cancelled for internal
  causes (`m.cancelRun`), so a synchronous `Persist` already has it durable when
  the cancel takes effect. External causes (the CLI signal, serve) are recorded as
  soon as the coordinator observes them; "persisted before acted on" for a loopback
  API cancel needs a runtime-owned cancel entry point (a method on a run handle or
  a request channel in `Options`), a decision for the F3.2/F4.6 owners.
- Reconstructing reason and leaked count from the store needs only the schema 13
  payloads from T5.
- Crash records of owned processes (pid plus start time), orphan sweep on start,
  and the run deadline after restart (spec Q8) belong to F3.3 and F3.2.

## Rebase points with in-flight branches

| Branch | Overlap | Rule |
| --- | --- | --- |
| F3.1 stack: `claude/f3-1-runtime` -> `claude/f3-1-cli` -> `claude/f3-1-guard-and-docs` (not merged) | `scheduler.go` (`finish(ctx, to)` signature, new `Options` and `runMachine` fields), `batch.go` (`prepareIsolated`), `accept.go` (`integrateIsolated`), `preflight.go` (`preflightIsolation`), `domain/run/result.go` (`Isolation`), `workspace.go`, `cmd/agentplane/{cli,serve,json,output}.go` | T1 to T6 touch none of these (T4 touches `workspace.go` only at the one `git rev-parse` call). **Do not start T7 until F3.1 T3 (`claude/f3-1-runtime`) is on `main`**; T8 waits for F3.1 T4 (`claude/f3-1-cli`). If they slip, branch T7 from `claude/f3-1-guard-and-docs` and retarget. Rebase work: `finish` already takes `ctx`, so the run reason is read from machine state, not a new parameter; add the abandoned-node retention rule to `releaseIsolated`; merge `Result.Isolation` and `Result.Reason` side by side |
| F2.8b: `claude/f2-8b-plan` (schema 12, `attempt_diff`) | `internal/domain/events/event.go` (`SchemaVersion`, `Validate` switch), `runtime/{scheduler,preflight,loop,batch,accept}.go`, `cmd` flags | T5 takes 13 (proposed; confirmed at merge) per the decided order above. In `accept.go`, F2.8b captures an attempt diff at attempt boundaries: the capture must be skipped when the attempt ended `timeout` or `cancelled` (add the check when whichever lands second rebases, and tell the F2.8b owners now) |
| F3.2: `claude/f3-2-plan`, PR #108 (spec only) | later: `Collector`/`Persist`, `internal/store`, `serve.go` | no code overlap today; the Durable criteria section lists the hand-off |
| F3.8/F3.9: `claude/f3-8-f3-9-plan` | later: process start path (Landlock re-exec, network namespace `Cloneflags`) | design decision 5: `proctree.Start` merges into a caller-prepared `SysProcAttr`; `TestStartKeepsCallerSysProcAttr` guards it |
| Issue #87 and the `git` `LookPath` issue | Windows test failures, `internal/gitstate/snapshot.go`, `runtime/workspace.go` | gating prerequisites for T12 only; this plan does not fix them |

Order of landing that minimizes conflicts: T1 to T6 any time (they are additive
and mostly new files); F3.1 T3, T4 and T5; then T7, T8, T9 (rebase once, onto the
F3.1 result); then Phase C. If F2.8b merges between T7 and T9, take the rebase in
T7's branch, not later.

## Decisions to confirm before T7

1. **`sibling_failed` scope.** Today an unrelated failure never cancels in-flight
   siblings: the batch drains, and the only nodes ended `cancelled` because of a
   failure are retry-pending nodes. The plan gives those the reason
   `sibling_failed` and does not add active abandonment of running siblings (that
   would change F0.4 scheduling). If the spec means the latter, it is a separate
   decision with its own tests.
2. **No HTTP cancel endpoint.** The `user_cancel` reason is produced by
   `serve`'s per-run cancel function; the API has no cancel route today. Adding one
   is F4.6 or an API slice.
3. **Effective run limit not in the trace.** `run_started` does not record the
   deadline in effect (graph limit or `--run-timeout`). "Trace is part of
   correctness" argues for an optional `limits` field on `run_started`. The plan
   omits it because the spec does not list it; add in T5 if the owner agrees.
4. **`Leaked` on macOS** is a lower bound of 1; exact count needs a process-table
   walk that the standard library does not offer. Accepted unless macOS counts
   matter.
5. **Abandon bound value.** `CancelBound` defaults to grace plus the 1 s verify
   bound plus 1 s. A shorter bound risks abandoning a healthy executor that is
   still terminating its tree.
6. **Windows assignment strategy** (`CREATE_SUSPENDED` with a thread snapshot
   versus `JOB_LIST` with raw `CreateProcessW`): the plan starts with
   `CREATE_SUSPENDED` to keep `os/exec`; W2 decides.

## Risks and edge cases

- **Pid reuse on Unix.** Guarded by group lifetime (see Contracts); the residual
  window is recorded in the ADR. Never signal after the group is reported empty.
- **Abandoned goroutines.** The abandon path leaves a goroutine running by
  design. The observer guard and the isolation retention rule exist so it cannot
  corrupt the trace or a worktree; real process executors never hit it because
  `Terminate` is bounded.
- **`appendEvent` panics after `run_finished`.** Any new emitter (the watcher, a
  late process fact) must be joined or guarded before `finish`; T7 tests run under
  `-race -count=20`.
- **Gate versus node limit.** The embedded Gate runs on the run context with its
  own 30 s evidence bound, so a short node limit cannot fail an attempt before
  evidence exists; a very slow producer cannot starve its check.
- **Grace hides slow agent shutdown.** `elapsed_ms` is recorded in
  `process_terminated`; the human renderer prints "forced after Ns".
- **Windows console state.** `CTRL_BREAK_EVENT` shares console state; mitigated by
  group scoping and the force fallback (W5). EDR hooks may delay termination: the
  verify bound is reported, not widened.
- **Windows file-name quirks in the in-process Gate compare** (8.3 names, case,
  `EvalSymlinks` equality): covered by normalized fixtures and the issue #87 work.
- **Schema bump.** Desktop, API and store readers must ignore unknown fields and
  types; T5 adds the reducer test and a store round trip.
- **Spec drift in the spike.** The spike was run on Windows 11 and WSL2 only; the
  GitHub `windows-latest` runner is untested, so T10 and T12 verify on the first
  CI run and the plan does not claim runner behavior.

## Definition of done for F3.4 and F3.4a

Every "Can ship before F3.2" criterion in the two specs is covered by a named test
above; `make check` and `go test -race ./...` pass in CI on Linux and macOS;
W1 to W7 pass on the blocking Windows job; the manual W8 record exists; docs,
indexes and the ADR number are consistent; the durable criteria remain explicitly
open until F3.2 lands.
