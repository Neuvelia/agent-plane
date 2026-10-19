# ADR-0011 - Bounded normalized progress and live delivery

**Status:** accepted for F1.3 implementation
**Date:** 2026-10-19

## Context

F1.2 observes sanitized statuses and Git evidence. The collector retains all
logs and synchronously delivers live facts, so a slow reader blocks scheduling.
F1.3 requires typed progress, bounded progress storage and explicit omissions.

## Decision

Emit event schema 3; decode existing schemas 1/2/3. node_evidence requires
schema >=2; new progress/omission types require >=3. Existing shapes retain
meaning; CLI envelope stays v1. Adapters supply typed payloads, scheduler owns
envelope correlation; attempt IDs stay empty until F1.6.

Normalized types: text_delta (source/item ID, public text), tool_call_started
and tool_call_finished (call ID, bounded tool name/status), command_started
and command_finished (command ID/status/optional exit code), file_changed
(path/action/reported provenance), status (finite code). Never expose private
reasoning, command argv, raw output, tool arguments/results or provider errors.
File progress is reported data, distinct from final observed Git evidence.

Fixed per-run progress retention is 4 MiB encoded whole-event JSON / 4096
items. Count log_chunk and normalized progress. Fixed internal live FIFO is
1 MiB encoded whole-event JSON / 1024 items INCLUDING its in-flight head.
Normalized payload content is capped at 16 KiB before JSON; legacy Shell raw
chunks keep their 16 KiB limit. Redact before splitting large text; omit an
oversized structural record rather than silently truncate. No raw debug data
is retained. Lifecycle and node_evidence retention is independent of progress
budgets and proportional to graph/evidence size; total memory and legacy
Shell capture are not claimed constant.

Use drop-new retention on either cap. A dedicated authoritative sequence
advances for validated facts even when progress is omitted, so retained gaps
are expected. A bounded forwarding FIFO and one pump ensure Append never
waits for a live reader. On first enqueue overflow seal live admission,
preserving delivery as a sequence prefix. Completion/cancellation stops and
joins the pump before Run returns; callers still own channel closure.

Finalization stops/joins delivery, freezes counters, retains one
progress_omitted marker if needed, then run_finished. These final records are
replay-only. Marker distinguishes retained omitted event/encoded-byte counts
from live omitted event/encoded-byte counts and live-stopped status. Live
counts cover ordinary pre-finalization attempts, excluding replay-only marker
and run_finished. Live-missed lifecycle facts remain retained. CLI replays
retained sequence > last delivered, without duplication; missing progress
cannot be recovered and the omission marker says so. RunResult.Events is now
complete lifecycle/evidence plus bounded progress and omission evidence.

Add typed progress callback to the observed executor seam and an optional
normalized adapter interface, preserving AgentAdapter and legacy callbacks.
Close callbacks/flush redaction before node termination; ignore late calls.
Redact every exposed string field; incremental text redaction must catch
boundary-spanning secrets with bounded logical-stream state (no unbounded
maps keyed by provider IDs). Normalized text holds only undecidable
secret-prefix and UTF-8 tails, admits at most 16 simultaneously unresolved
source IDs and releases slots when tails empty. While full, ignore new sources
and emit one finite `progress_limited` status; never retain a rejected-ID map.
Structural invalid/oversize records and admission-limited callbacks are
normalization omissions, distinct from collector counters for validated
admitted facts. Legacy Shell redaction/capture semantics remain unchanged.
Normalized string observation requires UTF-8-safe redaction configuration.
For an observed executor, reject invalid UTF-8 secret values or an invalid
UTF-8 replacement marker at run preflight, before any execution/event. A
byte absent from every secret remains the marker invariant; if that byte
cannot form valid UTF-8, fail safely rather than let JSON replace it with
U+FFFD (which could itself be a configured secret). Reject invalid UTF-8
normalized payload strings before serialization. Legacy byte-only log
executors retain arbitrary-byte redaction and base64 log serialization.
Scheduler remains provider neutral.

Codex maps verified supported item messages to normalized payloads. Completed
public assistant text may be a text_delta; this does not claim token-level
streaming. Drop reasoning/unknown data. Skip malformed optional progress;
drain oversized records to newline using bounded storage, then continue.
Explicit provider failure, process failure and invalid required final result
still fail. Preserve F1.2 process/environment/Git/result boundaries.

## Alternatives considered

- Blocking delivery: rejected because an absent reader stalls execution.
- Unlimited queues: rejected because long sessions exhaust memory.
- Guaranteeing all live lifecycle delivery with a fixed queue: impossible for
  arbitrary graph size and nonreading consumers; retained replay is explicit.
- Dropping old retained events: complicates prefix replay; drop-new and visible
  counters keep original retained progress and authoritative event order.

## Consequences

Consumers see typed observable progress and explicit gaps. Slow/no readers
cannot block scheduling. Lifecycle/evidence remains complete in memory, while
progress can be truncated. O(graph) lifecycle and evidence memory remains;
durable storage and large artifacts are later features.

## Compatibility and migration

Old v1/v2 records remain readable. Newly emitted events require schema3-aware
consumers. Legacy executors/adapters/Shell graphs remain valid. CLI watch uses
existing prefix/tail replay with omission rendering; no NDJSON mode is added.

## Validation

All normalized types round-trip with scheduler IDs. Barrier CLI test proves
progress before completion. At least 100 MiB generated progress with no reader
must respect byte/item high-water limits, preserve terminal facts and report
loss. Full-queue cancellation, slow-reader prefix/replay, boundary redaction,
reasoning exclusion and malformed-progress recovery are deterministic tests.
Run targeted suites, make check and go test -race ./... before review.
