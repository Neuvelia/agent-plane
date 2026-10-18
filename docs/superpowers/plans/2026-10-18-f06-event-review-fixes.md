# F0.6 Event Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the F0.6 review findings for split-secret logging, blocking caller callbacks, and invalid event type/payload pairs.

**Architecture:** Replace the arbitrary log sanitizer callback with an explicit list of byte strings to redact. A run-local, per-stream redactor retains enough trailing bytes to find matches across writes, then emits bounded chunks through the existing collector. A replacement marker uses a byte absent from all configured secrets, so the marker cannot reveal one. Validate the event type/payload pairing before a collector accepts an event or JSON is produced.

**Tech Stack:** Go 1.23 standard library; existing `go.yaml.in/yaml/v3` dependency only.

**Spec:** `docs/features/F0.6-event-model.md`; durable decision record `docs/adr/0006-event-model-contract.md`.

## Global Constraints

- Work on the current `feat/f06` branch and remain within F0.6. No persistence, replay, live subscriptions, retries, UI, or cloud work.
- Preserve run/node IDs, event names, per-run sequences, and scheduler transition ownership.
- Shell stdout and stderr remain distinct, byte accurate except for configured redactions, and limited to 16 KiB per event.
- Do not embed environment variables, graph inputs, command configuration, or raw error text in events.
- Run `make check`, `go test -race ./...`, and inspect the branch diff before completion.

## Current State and Data Contracts

`runtime.Options.LogSanitizer` is invoked for each observed write inside an `os/exec` pipe-copy goroutine. It has neither stream identity nor buffering, so a secret split between writes can escape; a blocked callback can prevent pipe draining. `events.Event` has a closed set of payload types, but its exported `Type` and `Payload` fields can disagree and JSON decoding then loses information. The run-local collector and F0.4 scheduler transitions already provide correct lifecycle and sequence ordering.

## Files and Responsibilities

- `internal/runtime/log_redactor.go` and `log_redactor_test.go`: deterministic byte-stream redaction, with independent stdout/stderr buffers and final flush.
- `internal/runtime/scheduler.go`: route Shell log callbacks through the redactor and collector; replace `LogSanitizer` with `LogRedactions []string`.
- `internal/shell/executor_test.go` and `internal/runtime/events_test.go`: prove full log reconstruction, correlation, split-secret handling, and cancellation.
- `internal/events/event.go`, `emitter.go`, and their tests: validate type/payload pairs and report rejected appends explicitly.
- `docs/features/F0.6-event-model.md`, `docs/adr/0006-event-model-contract.md`, and `internal/events/doc.go`: document the changed redaction and rejection contracts.

## Review Focus

1. A secret divided at an arbitrary write boundary must never appear in event bytes; Task 1 tests both stdout and stderr.
2. Normal bytes next to a redacted secret must remain in order exactly once; Task 1 tests reconstructed streams.
3. A child with large stdout and stderr must finish without a caller callback on a pipe goroutine; Task 2 uses the existing helper process.
4. Every log must carry its own node-run ID when two nodes execute concurrently; Task 2 uses synchronized workers.
5. A mismatched event type/payload pair must be rejected rather than serialized or silently decoded; Task 3 tests collector and JSON behavior.

---

### Task 1: Deterministic stream redactor

**Files:** Create `internal/runtime/log_redactor.go`, `internal/runtime/log_redactor_test.go`.

**Interfaces:** Produce `newLogRedactor([]string) (*logRedactor, error)`, `(*logRedactor).Write(events.Stream, []byte) []byte`, and `(*logRedactor).Flush(events.Stream) []byte`. The caller serializes calls with a mutex; `Write` returns bytes that are safe to emit and retains at most `maxSecretLength-1` undecided raw bytes per stream. `Flush` resolves the remainder after the executor has completed all callbacks. Overlapping secrets use the longest match first. Empty redaction strings are ignored. Construction fails when the secrets cover every byte and no safe marker exists.

- [x] **Step 1: Write failing tests.** Test a secret split as `"sec"` and `"ret"` across two writes, separately on stdout and stderr. Also test text before/after the match, overlapping redactions, empty redaction strings, and unchanged bytes when no redactions are configured. Assert on reconstructed output, not on callback counts.
- [x] **Step 2: Verify red.** Run `GOCACHE=/private/tmp/agentplane-f06-go-cache go test ./internal/runtime -run 'TestLogRedactor' -count=1`; expect a compile failure for the absent redactor API.
- [x] **Step 3: Implement the minimum redactor.** Keep per-stream pending bytes. At each offset, wait until the longest configured secret could be fully observed, choose the longest matching secret, emit a marker byte absent from every secret three times, or emit the next unmatched byte. At flush, resolve every remaining byte. Never return an undecided suffix.
- [x] **Step 4: Verify green.** Run the targeted test command again; expect pass.

### Task 2: Connect redaction to scheduler events

**Files:** Modify `internal/runtime/scheduler.go`, `internal/runtime/events_test.go`, `internal/shell/executor_test.go`.

**Interfaces:** Consume Task 1's `logRedactor`. `Options.LogRedactions []string` replaces `Options.LogSanitizer`; only deterministic redaction and collector append run in the Shell observer. Flush stdout and stderr after `ExecuteNodeWithLogs` returns and before the worker sends its completion to the coordinator.

- [x] **Step 1: Write failing integration tests.** Use a `LogStreamingExecutor` test double that splits a secret between callbacks, writes both streams, and runs two synchronized nodes concurrently. Assert complete stream reconstruction, correct node-run IDs, monotonic sequences, and logs before node finish. Adapt the Shell helper test to `LogRedactions` and add a large-output integration assertion.
- [x] **Step 2: Verify red.** Run `GOCACHE=/private/tmp/agentplane-f06-go-cache go test ./internal/runtime ./internal/shell -run 'TestRunRedactsSplitSecrets|TestConcurrentRunLogCorrelation|TestFailedShellRunEmitsLogsBeforeTerminalEvents|TestLargeShellRunLogs' -count=1`; expect failure against the old callback behavior or absent option.
- [x] **Step 3: Implement the minimum scheduler change.** Construct one redactor per worker, serialize writes to its two stream states with one mutex, emit only resolved bytes in 16 KiB chunks, and flush both streams before returning the worker result. Copy configured redaction strings before starting workers.
- [x] **Step 4: Verify green.** Run the targeted command and `go test ./internal/runtime ./internal/shell`; expect pass.

### Task 3: Reject inconsistent event envelopes

**Files:** Modify `internal/events/event.go`, `internal/events/emitter.go`, `internal/events/event_test.go`, `internal/events/emitter_test.go`, and scheduler append calls if the collector signature changes.

**Interfaces:** Add `Event.Validate() error` for the exact `Type`/concrete `Payload` pairing. `Event.MarshalJSON` must return an error for an invalid pair. `Collector.Append` must report invalid input and post-terminal appends explicitly rather than returning a zero event without explanation. Keep the existing run-local mutex and sequence assignment. Runtime-created events remain valid.

- [x] **Step 1: Write failing tests.** Try `LogChunk` with `NodeFinishedPayload` through JSON and collector; expect descriptive errors and no stored event. Try append after `RunFinished`; expect an explicit error and an unchanged snapshot. Keep valid payload round-trip tests green.
- [x] **Step 2: Verify red.** Run `GOCACHE=/private/tmp/agentplane-f06-go-cache go test ./internal/events -run 'TestEventRejectsMismatchedPayload|TestCollectorRejectsInvalidAndLateEvents' -count=1`; expect failure because mismatches currently serialize and late appends return a zero event.
- [x] **Step 3: Implement validation.** Validate the sealed payload type against the event type, return errors from invalid collector appends, and call validation in custom JSON marshaling. Adapt scheduler append sites to treat an internal validation error as an invariant failure rather than silently recording a zero sequence; no caller-supplied sink is introduced.
- [x] **Step 4: Verify green.** Run the targeted command and `go test ./internal/events ./internal/runtime ./internal/shell`; expect pass.

### Task 4: Update contracts and verify the branch

**Files:** Modify `docs/features/F0.6-event-model.md`, `docs/adr/0006-event-model-contract.md`.

**Interfaces:** Document explicit redaction strings, per-stream buffering and final flush, limits of redaction when none are configured, and collector rejection semantics. No event JSON field or event name changes.

- [x] **Step 1: Update the spec and ADR** to match Tasks 1–3 and remove the old callback claim.
- [x] **Step 2: Run `GOCACHE=/private/tmp/agentplane-f06-go-cache make check`.** Expect `check: OK`.
- [x] **Step 3: Run `GOCACHE=/private/tmp/agentplane-f06-go-cache go test -race ./...`.** Expect every package to pass with no race report.
- [x] **Step 4: Inspect `git diff --stat`, all changed files, and `git diff --check`.** Confirm no unrelated changes, no later-phase code, and no temporary files.

## Risks and Ambiguities

- F0.6 requires a redaction seam, not automatic discovery of secrets. The explicit list defines which byte strings to redact; child output not on that list remains visible.
- Redaction changes event log bytes but does not alter F0.5's local process evidence or accepted node outputs. This is the existing trust boundary of `RunResult` versus event payloads.
- User callbacks are removed from the pipe path. A future general transformation API would need its own cancellation and delivery contract.
- The collector's public rejection API is a small compatibility change on this unmerged feature branch. Scheduler-generated events should never be rejected; tests cover this invariant.

## Review Correction

The independent review found that the initially planned literal `[REDACTED]` marker could contain a listed secret or create one at a marker boundary. The implementation uses a repeated byte absent from every listed secret and rejects a configuration with no safe marker byte before starting a run. The regression test observed both leak scenarios before this correction.
