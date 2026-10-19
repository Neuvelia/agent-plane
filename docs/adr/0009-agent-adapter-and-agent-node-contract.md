# ADR-0009 - Agent adapter and Agent node contract

**Status:** accepted for F1.1 planning
**Date:** 2026-10-19

## Context

Phase 0 has a generic node executor, schema-v0 `config`, lifecycle events and
run/node IDs. The roadmap describes capability detection, preparation,
execution, streaming, cancellation and output collection for agents. F1.1 must
establish a provider-neutral contract without implementing F1.2's real CLI or
F1.3's normalized stream. These decisions are durable because later adapters
and authored graphs will depend on them.

## Decision

### Adapter interface and ownership

The minimum Go adapter contract has stable `ID`, `Detect(ctx)` and
`Execute(ctx, input) (output, error)` operations. `Detect` distinguishes an
unavailable adapter from a detection error and reports known/unknown
capabilities. Preparation and output collection are internal steps of
`Execute`; F1.1 has no separately observable prepare/collect lifecycle.
Cancellation is through the execution context. An explicit `Cancel` method
would duplicate this contract without a process/session to cancel in F1.1.
F1.2 must prove process/session termination, and may add a justified optional
interface through a new ADR if context alone cannot express it. The unified
event vocabulary and stream delivery are reserved for F1.3. F1.1 may pass a
minimal callback through its executor seam but cannot expose provider event
types as the runtime contract.

`internal/runtime` owns scheduling, binding resolution, identity creation and
lifecycle events. A caller-owned Agent executor resolves adapters by logical
ID from an explicit registry and converts graph values to typed adapter
input. The runtime never branches on a provider name. A narrow optional
correlated-executor interface passes the existing `RunID` and `NodeRunID` to
compatible executors; legacy Shell execution remains valid. `AttemptID` stays
empty until F1.6 actually creates attempts. The adapter cannot mint or replace
these IDs.

### Agent node in schema v0

`type: agent` uses the existing opaque node `config`; no schema-version bump or
new top-level field is required. F1.1 defines `config.adapter` as a required
nonempty logical ID and `config.workspace` as a required absolute local workspace
path. F1.1 validates the path shape; F1.2 owns real process access checks.
The task comes from exactly one source: a literal `config.task` or a declared
string `task` input resolved through an ordinary graph binding. Both/neither
are errors. Optional `allowed_paths`, `context_refs` and `environment_refs`
are typed, explicit lists in `config`. They are inert data: F1.1 neither
reads referenced content nor resolves secrets or environment values. Unknown
Agent configuration keys and wrong types fail before adapter execution. The
node's supported output ports and types are documented in `docs/graph-spec.md`.
The graph validator retains Phase 0 generic semantics; the Agent executor
performs node-specific validation at execution preflight. This means graph
validation alone cannot certify Agent configuration in F1.1.

### Result and evidence

The adapter returns a typed normalized result: summary, changed-file paths,
artifact references, opaque JSON provider metadata and optional usage values.
Unknown usage is absent, distinguishable from a reported zero. Provider
metadata is validated JSON and not interpreted by the runtime. Only explicitly
declared output ports become accepted graph values; mismatched or unsupported
ports fail the node. This preserves the existing edge/data dependency model.
Large output and artifact storage are later features.

F1.1 uses F0.6 lifecycle events with the same schema version and
run/node correlation. It does not put task text, raw provider metadata,
environment values or full outputs in lifecycle payloads. The in-memory
`RunResult` carries accepted node outputs and structured failure; event-only
reconstruction of Agent output is not promised by F1.1. F1.2 must make changed
files visible in trace through a separately specified result/evidence event,
and F1.3 defines normalized progress events, retention and backpressure. An
event schema change needs its own ADR before implementation.

### Scope and trust

`allowed_paths` is advisory task data in F1.1. It must not be described as a
filesystem sandbox, permission boundary or proof of changed-file scope. F1.4
can check an explicit reported path list; F1.8 adds Git-derived paths and
warn/deny enforcement. F3.1 owns isolated workspaces. No implicit parent
environment forwarding or repository-content discovery is permitted.

## Alternatives considered

- A six-method adapter matching every roadmap verb immediately: rejected
  because F1.1 lacks a real process and a settled stream vocabulary; it would
  freeze speculative method signatures.
- Provider branches in the scheduler or a global adapter registry: rejected
  because they couple core execution to a CLI and complicate deterministic
  tests.
- Treating `allowed_paths` as an enforced boundary: rejected because F1.1
  has no Git check, sandbox or workspace isolation.

## Consequences

The first real adapter can be developed against a small stable input/output
contract. F1.2/F1.3 may need additive event and process-lifecycle seams, each
reviewed against this ADR. Graph authors must use the Agent configuration
convention and cannot rely on generic `graph validate` to catch all Agent
configuration errors yet. F1.1 does not provide event-only replay of Agent
results or guaranteed file-scope enforcement.

## Compatibility and migration

Existing schema-v0 Shell graphs, executor implementations and F0.6 lifecycle
event serialization remain valid. Agent config is additive within `config`.
If later features change its durable shape or event payloads, they require a
new ADR and an explicit migration/compatibility rule.

## Validation

F1.1 contract tests cover capabilities, typed input/output, unknown usage,
errors and cancellation; a runtime integration test proves generic Agent
dispatch, the same IDs in adapter input and lifecycle events, graph output
binding and unchanged Shell behavior. F1.2/F1.3 specs and tests cover real
process cancellation and observable streaming separately.
