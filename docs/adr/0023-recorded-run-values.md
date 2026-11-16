# ADR-0023 - Recorded run values for edge inspection

**Status:** accepted (recording is opt-in, off by default: decided by the owner on 2026-11-15)
**Date:** 2026-11-14
**Implementation:** engine slice E6 implemented with the opt-in default
(`docs/features/F2.3-E6-recorded-run-values.md`). Interpretations recorded in
that spec and kept: `size_bytes` and `sha256` describe the full value after
redaction, `value` is always a string in a per-type text form, text that is
not valid UTF-8 is recorded without `value`, nothing is recorded on the target
side, and values are read through the existing events stream. The desktop
Settings switch and sidecar flags are wired; the edge inspector is slice E5b.

## Context

Decision 8 of the graph editor plan requires F2.3 to let a user point to the concrete value that passed
through an edge. No event or API carries values today. Verified: `attempt_input` has only a task fingerprint
and scope flags, `node_finished` has status and duration, `approval_requested` carries the human node's
prompt and context items. F1.7 keeps accepted runtime values and their provenance
(`AcceptedProvenance{run, node run, accepted attempt, port}`) in memory and returns them in `RunResult`,
but they are never published or stored.

Constraints from AGENTS.md: events are append-oriented facts and every transition is observable (rule 8);
never place raw secrets in events; do not log secrets; unknown is not zero; the UI derives state by reducing
the event stream (F2.5, ADR-0019). Values are repository, user and model data: they can be large and can
contain secrets the engine does not know about (redaction today works from an explicit list of configured
secret values, `runtime.Options.LogRedactions`, which `serve` does not yet expose, ADR-0019).

## Decision

1. **Record accepted output values as events.** A new event `value_recorded` (event schema **10**,
   additive; schema 1-9 streams stay readable) is emitted once per accepted output port when a node's output
   is accepted, in order before the node's terminal event, and once per graph input at run start. Payload:
   `node_run_id` and `attempt_id` (empty for graph inputs), `port` (for graph inputs `inputs.NAME`), the
   value `type` (string, number, boolean, json, artifact_ref), `size_bytes` of the full value, `sha256` of
   the full value, `truncated`, `redacted`, and `value` (see 3). Provenance is implied by the identifiers and
   matches `AcceptedProvenance`.
2. **An edge's value is the source output's recorded value.** An edge maps one source output to one target
   input with no transformation, so the value that crossed it is the value recorded for the source port (the
   graph input for a graph-input binding). The reducer joins the graph's edges with `value_recorded` events
   by source node and port. Nothing is recorded on the target side. Static units yield one value per source
   node run.
3. **Bounds.** At most 16 KiB of the value is carried per event (the same bound as log chunks); a larger
   value is cut at a valid boundary, `truncated` is true, and `size_bytes` and `sha256` still describe the
   whole value. A run records at most 1 MiB of values in total; after that, ports are recorded with
   `value` omitted and `truncated: true`, so absence is never confused with an empty value. An
   `artifact_ref` records the reference string only, never the artifact's contents.
4. **Redaction before recording.** Values go through the same redactor as logs and events (configured
   secret values replaced by the redaction marker, matches across the cut handled), and `redacted: true`
   marks a changed value. `serve` gains the redaction configuration it lacks today (`--redact-env`, as
   `agentplane run` has) **before** recording can be switched on from the desktop. A graph that contains a
   configured secret value is still refused for storage (ADR-0019).
5. **Recording is opt-in per server.** `agentplane serve` records values only when started with
   `--record-values`; the desktop passes it only when the user has enabled "Record values for edge
   inspection" in Settings, which is off by default. When off, `value_recorded` is never emitted and the
   edge inspector says values were not recorded for this run; it never shows "empty". The setting applies
   to runs started afterwards and never rewrites stored runs.
6. **Persistence and replay.** Events already go to `events.jsonl` (0600, in a 0700 run directory); values
   are part of those facts, so replay after a restart is identical to the live stream and the F2.5 rule
   (state is a reduction of events) holds with no second read path.
7. **The CLI** (`agentplane run --json`, `--store-dir`) records values only with an explicit
   `--record-values`, for the same reasons. The default CLI event stream is unchanged.
8. **Unknown is unknown.** A value that was not recorded (feature off, cap reached, run from an older
   schema) is shown as such. No value is invented for failed or skipped nodes: only accepted outputs are
   recorded.

## Open decision (product owner)

The plan left open whether recording is opt-in or default-on. This ADR proposes **opt-in, off by default**
because values are user data that the explicit-list redaction cannot fully protect, and because AGENTS.md
forbids secrets in events. The cost is that edge inspection needs a one-time switch. If the product owner
prefers default-on, decision 5 changes to default-on with the same switch to turn it off; decisions 1-4 and 6-8
do not change.

**Decision (owner, 2026-11-15):** opt-in, off by default, as proposed. Decision 5 stands unchanged.

## Alternatives considered

### A. Values in a side store, referenced by events

Pros:

- Keeps the event stream small; values could be deleted independently.

Cons:

- A second storage format, a second read path and a second API; the UI could no longer derive everything by
  reducing events; replay consistency becomes a separate problem. Rejected for the first version; bounded
  values (decision 3) keep events small enough.

### B. Do not record; show only declared types statically

Pros:

- No new contract and no new data exposure.

Cons:

- Does not meet F2.3's criterion (point to a concrete value). Kept as the behaviour when recording is off.

### C. Record every input and output of every attempt, including rejected attempts

Pros:

- Richer debugging.

Cons:

- Multiplies volume and exposure; rejected attempts are already explained by check results (F1.5).
  Only accepted outputs are needed for edge inspection. Out of scope.

## Consequences

### Positive

- The UI can show the concrete value on an edge from the stored events, live and after a restart.
- The exposure is explicit, bounded, redacted and off unless the user enables it.

### Negative / trade-offs

- Events can carry user and repository data when enabled; `events.jsonl` becomes more sensitive and there is
  still no retention or deletion tool (F3.2).
- Redaction is explicit-list only: an unknown secret in a value is recorded. This is why recording is
  opt-in and must be explained next to the switch.
- A new event type and schema version touch the emitter, the store reader and the desktop reducer.

## Compatibility and migration

Event schema 10 is additive; consumers that ignore unknown event types keep working (the desktop reducer
already does). Streams recorded with schema 1-9 contain no values and show "not recorded". The default
behaviour of `agentplane run`, `serve` and stored runs is unchanged.

## Validation

- With recording off, no `value_recorded` event appears in any stream (test on the recorded demo graphs).
- With recording on: one event per accepted output port and per graph input; values above the cap are
  truncated with the correct `size_bytes` and `sha256`; the per-run cap omits further values with
  `truncated: true`; a configured secret never appears in an event, including across the truncation cut.
- Replay from the store reduces to the same edge values as the live stream.
- A failed, skipped or cancelled node records nothing; a rejected attempt's output is never recorded.
- The desktop shows "not recorded" when the feature was off or the schema is older, and never an empty
  value in its place.
