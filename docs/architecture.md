# Architecture

## Purpose

AgentPlane is a local-first execution layer and debugger for AI coding agents and deterministic tools.

The architecture separates the shape of work from the local correction of work:

- **Graph**: what units exist, which units depend on which outputs, which units can run concurrently, where results merge, and where execution is gated.
- **Loop**: produce, check, correct, repeat for a single bounded unit until it is accepted or its retry budget is exhausted.

## Product boundaries

AgentPlane should integrate existing coding agents rather than replace them.

It should not become:

- a foundation model provider;
- a generic no-code automation product;
- a project management suite;
- a mandatory cloud service;
- a system where every transformation is delegated to an LLM.

## Architectural layers

The diagram below describes conceptual ownership. The current Go packages,
loopback API, run store and desktop client are mapped in
`docs/codebase-map.md`.

```text
+-----------------------------------------------------------+
| Future UI / IDE                                           |
| Graph editor, live execution map, run inspector           |
+-----------------------------+-----------------------------+
                              |
                              v
+-----------------------------------------------------------+
| Execution Engine                                          |
| Graph validation, scheduler, state, loops, gates          |
+-----------------------------+-----------------------------+
                              |
                              v
+-----------------------------------------------------------+
| Local Runner                                              |
| Processes, workspace access, artifacts, secrets, events   |
+-----------------------------+-----------------------------+
                              |
                              v
+-----------------------------------------------------------+
| Execution Adapters                                        |
| Shell, Codex, other agent CLIs, future HTTP/MCP/Docker    |
+-----------------------------+-----------------------------+
                              |
                              v
+-----------------------------------------------------------+
| Local repository / isolated workspace                    |
+-----------------------------------------------------------+

Optional later:

Cloud Control Plane <---- outbound connection ---- Local Runner
```

## Trust boundaries

### Local runtime

The runner can access code, processes, and explicitly granted environment values. It is therefore high-trust infrastructure executing low-trust instructions.

The runtime must assume that graph definitions, agent output, commands, paths, and future remote control messages can be malformed or hostile.

### Cloud control plane

Cloud is optional and should not require repository source to leave the execution environment.

The runner should initiate outbound connections. A user should not need to expose an inbound port on a laptop.

## Execution model

A node has a bounded responsibility and an explicit contract.

```text
NodeInput -> execute -> NodeOutput
```

An edge maps a specific output to a specific input.

```text
planner.outputs.backend_units
            |
            v
backend.inputs.units
```

The runtime should not rely on hidden shared conversational context between nodes.

## Node types

Phase 0 requires only Shell Node for real execution while graph/runtime contracts are proven.

Later node types:

- Agent
- Gate
- Human
- Code
- Splitter
- Merge
- Router
- Subgraph

A node type should exist because it has distinct execution semantics, not because it looks different in the UI.

## Local correction loop

The later agent loop is:

```text
produce
   |
   v
 check ---- green ----> accepted
   |
  red
   v
correct
   |
   +---------------> produce
```

Requirements:

- deterministic check where possible;
- bounded attempts;
- explicit reason and evidence;
- correction scope;
- attempt history;
- escalation after exhaustion.

## State model

Target node lifecycle:

```text
PENDING
  -> READY
  -> RUNNING
  -> CHECKING
      -> SUCCEEDED
      -> CORRECTING -> RUNNING
      -> WAITING_FOR_HUMAN
      -> FAILED
      -> CANCELED
```

Phase 0 may use the smaller scheduler state set:

```text
pending -> ready -> running -> succeeded|failed|cancelled
    |         |
    |         +-----> skipped|cancelled
    +---------------> failed|skipped|cancelled
```

F0.4 keeps ordered in-memory transition facts. F0.6 adds structured events
and correlation IDs from those same transitions.

## Events

The runtime uses append-oriented execution events so the execution path can be reconstructed.

Minimum early events:

```text
run_started
node_ready
node_started
log_chunk
node_finished
node_terminated
run_finished
```

An unstarted node that fails, skips, or cancels emits `node_terminated` instead
of `node_finished`. Events have a per-run sequence assigned by one in-memory
collector. The sequence, rather than timestamp, is authoritative for order.
Completed runs expose complete lifecycle/evidence plus bounded progress and
omission evidence. F1.3 schema 3 adds normalized public progress and a bounded
live FIFO prefix; an absent reader cannot block scheduling. Retained progress
is limited to 4 MiB encoded event JSON / 4096 items and live delivery to 1 MiB /
1024 items including its in-flight head. Finalization joins the delivery pump
before retaining omissions and run_finished for replay. Lifecycle/evidence and
legacy Shell capture are outside these progress budgets; total memory is not
constant. Durable delivery remains later work. See ADR-0011.

F1.4 schema 4 adds a bounded typed `gate_check` fact, retained independently
of progress budgets on green and red before terminal lifecycle. Operational
failures have no verdict. F1.5 emits schema 5 with the full version-1 CheckResult,
including required nonnegative duration_ns, checked-input scope and typed
references. Readers accept schemas 1–8; schema 4 Gate facts remain a distinct
legacy payload with no invented metadata. Full results require schema 5.
`events.CheckResults(trace, runID, nodeRunID)` returns detached current results
in sequence order, including red; legacy inspection uses
`events.LegacyGateCheckPayload` from the event payload. Gate facts remain
independent of progress budgets. See ADR-0012 and ADR-0013. Later events include
correction, artifact, and approval events.

The event stream is an observable record of execution, not a container for hidden model reasoning.

F1.6 emits schema 6 and adds retained `attempt_started`, `attempt_input`, and
`attempt_finished` facts for node-local loops. Input summaries contain the
SHA-256 task fingerprint, a redacted bounded intended scope, reference-presence
and omission flags, and previous-result linkage. They omit raw tasks, reference
contents, environment values and private reasoning; they do not promise replay.
Scope summaries retain at most 32 entries, 512 bytes per entry and 1024 total
unescaped bytes; the event payload is bounded to 8192 encoded bytes.
Attempt outcomes are accepted, rejected, needs_intervention, execution_error
or cancelled. Each invocation closes and flushes observers before returning;
callbacks arriving later are discarded. Logs, progress, checks and producer
evidence carry the current AttemptID. Attempt lifecycle and checks are retained
independently of progress limits.

Readers accept schemas 1–8. Full CheckResults in schema 5 through 8 remain
available through `events.CheckResults`; `events.AttemptCheckResults` adds a
detached attempt filter and does not invent attempts for older facts. Graph
schema 0 remains additive, nil-loop hashes stay unchanged, and CLI envelope
schema remains 1. Exhausted loops fail with needs_intervention and CLI exit 4.
Cancellation prevents later correction dispatch; unrelated racing failures
retain their error cause. F1.6 adds no workspace isolation, rollback, scope
enforcement, sibling envelopes or recovery. See ADR-0014.

The new untrusted CheckResult feedback is bounded by the existing 32 KiB check
payload limit and is sanitized once before both retention and detached transport.
Original authored Task, allowed paths, and references keep their existing Agent
input contract and are copied without narrowing; total authored input memory and
the full feedback object including authored scope have no new global size cap.
The trace limits above apply only to its observable summary.

## Persistence direction

Phase 0 is in-memory.

Later local persistence should use:

- SQLite for metadata and event indexes;
- filesystem for larger artifacts/logs;
- YAML/JSON for graph definitions;
- Git for graph/repository version context.

A future durable event store should preserve append-only audit semantics even if snapshots are added for performance.

## Workspace isolation direction

The expected progression is:

1. current workspace during the earliest prototype;
2. Git worktree per unit/node;
3. process-level restrictions;
4. optional container isolation;
5. ephemeral VM/sandbox for managed execution.

Do not design Phase 0 APIs in a way that requires every node to share the same mutable working directory forever.

## Cloud separation

The execution engine must not import cloud concerns.

Cloud features are a later layer for:

- remote start;
- long-term history;
- approvals;
- teams;
- scheduling;
- policy;
- analytics;
- managed runners.

The local engine must remain useful without them.

## Static-unit acceptance (F1.7)

Explicit optional `units` enrolls static loop Agent nodes. The coordinator owns
registration, current attempts, ReturnEnvelope v1 derivation and accept-once
runtime port values/provenance. CheckResult → ReturnEnvelope → attempt_finished
are retained schema7 facts; operational errors invent no verdict. Existing
bindings read only accepted values. Rejected units retry within F1.6 budgets;
accepted siblings remain unchanged, including when another unit exhausts.

Unit-mode schedules every Agent's producer plus check as a protected cycle.
Only bounded controlled-environment read-only Git inspection can prove distinct
physical workspaces for concurrent Agent cycles. Shared/nested/aliased or
unknown identities serialize; distinct linked worktrees keep concurrency.
Declared-scope disjointness is insufficient. Potential conflicts are retained
as at most 64 structured details plus an omission count, outside progress limits.
Shell/external writers, concurrent hostile path changes and filesystem rollback
are outside this in-run rule. Accepted runtime values can stay fixed while
shared repository files change; F1.8 and F3.1 own enforcement/isolation.
See ADR-0015. Graph 0 and CLI 1 remain additive; absent units preserves legacy
hashes, large authored scopes and F1.6 scheduling behavior.

## Git-observed scope checking (F1.8)

An opt-in embedded Gate checks the Agent's declared paths against a baseline
captured before its first attempt and retained through retries. A shared
provider-neutral Git helper observes index, worktree, untracked (including
ignored) and deleted paths; old and new rename paths are both evaluated. Provider-reported
files are metadata only. Warn produces a green result with visible violation or
indeterminate evidence; deny produces red for violations and an operational
failure when inspection cannot complete. Schema 8 adds only typed details to
the existing CheckResult; the Return Envelope and correction loop use their
established path. This is post-execution detection, not filesystem isolation.
External writers can still alter the shared workspace. See ADR-0016.

## Workspace isolation facts (F3.1)

Event schema 11 adds five typed, bounded facts for isolated Agent units:
`workspace_prepared`, `workspace_integrated`, `workspace_conflict`,
`workspace_released` and the run-level `source_workspace_changed`. They are
retained outside progress limits, deeply copied and fsynced by the run store.
The events package only defines their shape; the runtime that emits them and
the Git operations behind them (`internal/worktree`) arrive in later F3.1
tasks. Readers accept schemas 1-10 and older streams contain none of these
facts. See ADR-0024.
