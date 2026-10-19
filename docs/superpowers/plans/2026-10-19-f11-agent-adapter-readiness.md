# F1.1 Agent Adapter Contract Implementation Plan

**Status:** approved implementation plan; implementation has not started.
ADR-0009 and `docs/graph-spec.md` record the durable contract.

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a provider-neutral agent contract and prove that a graph Agent node can execute through a deterministic mock without changing Phase 0 Shell behavior.

**Architecture:** Keep the scheduler responsible for lifecycle, bindings, and events. Put agent types and capability semantics in `internal/agent`; put graph configuration decoding and adapter selection in an agent executor outside `internal/runtime`. Use the existing `runtime.Executor` path, with a narrow optional correlated-execution interface so an agent receives the run and node IDs before execution.

**Tech Stack:** Go 1.23, standard library plus the existing YAML dependency, in-memory runtime.

**Spec:** `docs/features/F1.1-agent-adapter-contract.md`; ADR-0009 records
the interface, schema-v0 Agent config, correlation and event boundary.

## Readiness review (2026-10-19)

- Phase 0's three-node CLI test proves actual branch overlap, bound data delivery to a dependent Shell process, and a successful terminal result (`cmd/agentplane/integration_test.go`).
- `runtime.Run` already accepts a provider-neutral node `Executor`, resolves graph inputs, validates declared outputs, and emits ordered lifecycle events (`internal/runtime/scheduler.go`). There is no need to redesign scheduling for F1.1.
- The CLI passes `shell.Executor{}` directly, so it cannot run an Agent node today (`cmd/agentplane/cli.go`). F1.1 only requires runtime execution through a mock; keep the public CLI's existing Shell behavior until a real adapter is available.
- Schema v0 permits `type: agent` and opaque `config`, but it has no Agent configuration contract or adapter selection validation. The `graph.Validate` layer only checks that a node type is nonempty. Agent-specific checks belong at the executor boundary unless an explicit schema migration is chosen.
- The `runtime.Executor` signature has node ID but no run ID or node run ID. F0.6 reserves `AttemptID` and intentionally leaves it empty. An additive correlation seam is required; do not mint fake attempt IDs in F1.1.
- F1.1 was untracked at the start of this review. This documentation change
  adds it to Git and updates Phase 1 navigation.
- The roadmap mentions `prepare`, streaming, `cancel`, and output collection.
  ADR-0009 resolves these as internal steps of `Execute`, context cancellation
  and the separate F1.3 stream contract.

**Baseline evidence:** With `GOCACHE=/tmp/agentplane-go-cache`, `make check` passed. With `GOCACHE=/tmp/agentplane-go-race-cache`, `go test -race ./...` passed. The default Go cache path is outside this task's writable roots, so unmodified commands fail here before compiling.

## Scope and proposed decisions

- Define `AgentAdapter` with `ID() string`, `Detect(context.Context) (Capabilities, error)`, and `Execute(context.Context, ExecutionInput) (ExecutionOutput, error)`. Preparation remains inside `Execute`; cancellation uses its context. Add an explicit cancellation method only if F1.2 provides a concrete process/session need.
- `Capabilities` reports availability, optional version, and tri-state cancellation, streaming, usage reporting, and workspace support (`*bool` or an equivalent explicit unknown type). Unavailable and detection failure are distinct.
- `ExecutionInput` carries task, workspace, allowed paths, context references, environment references, and run/node/attempt correlation. References remain inert identifiers; no secret or file resolution happens in the contract.
- `ExecutionOutput` carries summary, changed files, artifact references, `json.RawMessage` provider metadata, and optional usage values with pointer fields so unknown differs from zero. Validate raw JSON before accepting it as a graph value.
- For a graph Agent node, use `config.adapter` for explicit local lookup,
  an absolute local path in `config.workspace`, and an explicitly declared
  string `task` input or literal `config.task` (reject both together and
  reject neither). Treat other configuration fields as typed, inert data.
  Do not interpolate strings or read ambient environment values.
- Return normalized fields through these optional declared graph output ports: `summary: string`, `changed_files: json`, `artifacts: json`, `raw_metadata: json`, and `usage: json`. Omit unknown optional values rather than inventing zero or `null` output values. The agent executor must reject an unsupported declared port/type instead of silently dropping it. The scheduler continues to decide whether the output contract passed. Keep raw metadata out of lifecycle event payloads.
- Give the agent executor a small map of registered adapter instances supplied by its caller. Do not add global registration, discovery, or a provider-specific branch in `internal/runtime`.
- Add one optional runtime interface that receives a typed execution identity (`RunID`, `NodeRunID`, empty `AttemptID`) alongside the existing executor arguments. `runtime.Run` calls it when implemented and otherwise keeps the current `Executor` behavior. The agent executor implements this interface; Shell remains unchanged.

## Global constraints

- No real provider, gate, retry, unified streaming, worktree, secret resolution, UI, cloud, or persistence in F1.1.
- Existing Shell graph files and CLI JSON/event schema must remain valid.
- Do not claim that advisory `allowed_paths` enforces write scope.
- Do not put task text, environment values, raw metadata, or provider output into lifecycle events by default.
- Follow ADR-0009 for durable Agent node configuration, normalized output
  ports, correlation and lifecycle-event limits.

## Review focus

1. Unknown adapter ID: report a node-scoped actionable error before invoking any adapter.
2. Missing, duplicated, or wrong-type task source: fail before adapter execution.
3. Unavailable adapter versus `Detect` error: preserve distinct failure causes.
4. Cancellation: the mock must observe context cancellation, and runtime must mark the node/run consistently without hanging.
5. Malformed provider JSON or unsupported output port: reject the node result without publishing accepted outputs or leaking raw metadata into events.

## File map

| File | Responsibility |
| --- | --- |
| `internal/agent/contract.go` | Adapter, capability, input, output, reference, usage, and error types. |
| `internal/agent/executor.go` | Typed Agent node configuration, adapter lookup, input construction, output-port mapping. |
| `internal/agent/contract_test.go` and `internal/agent/executor_test.go` | Contract and executor behavior, including failure cases. |
| `internal/agent/agenttest/mock.go` and `internal/agent/agenttest/conformance.go` | Test-only mock and reusable conformance suite; no production package imports. |
| `internal/runtime/scheduler.go` and `internal/runtime/scheduler_test.go` | Optional correlated executor dispatch and compatibility tests. |
| `internal/graph/yaml_test.go`, `internal/graph/version_test.go` | Agent config round trip, strict top-level fields, semantic hash changes. |
| `docs/graph-spec.md` and a new ADR | Public Agent node and execution contract decisions. |
| `docs/features/README.md`, `AGENTS.md` | Phase 1 navigation and current phase wording, if included in the documentation change. |

## Tasks

### Task 1: Freeze the F1.1 contract and schema seam

- [x] Record `AgentAdapter` methods, context cancellation, tri-state
  capabilities, graph config, output ports and correlation in ADR-0009.
- [x] Document the additive schema-v0 Agent configuration, task-source rule,
  declared output ports and advisory scope in `docs/graph-spec.md`.
- [x] Review the ADR and graph spec against F1.1 and the Phase 1 roadmap.

### Task 2: Define the provider-neutral adapter contract

- [ ] Write failing tests for identity, available/unavailable/unknown capabilities, known versus unknown usage (including a reported zero), and JSON metadata round trip.
- [ ] Run `GOCACHE=/tmp/agentplane-go-cache go test ./internal/agent/...`; expect the new tests to fail before implementation.
- [ ] Add the small typed API to `internal/agent/contract.go`; avoid maps at the stable boundary except opaque raw JSON.
- [ ] Run the targeted tests until they pass.

### Task 3: Prove the contract with a reusable mock suite

- [ ] Add a deterministic mock and exported conformance test helper under `internal/agent/agenttest`; tests configure capabilities, output, execution error, and a channel-controlled cancellation path.
- [ ] Add a consumer test that runs the conformance suite against the mock. Assert propagation of task, workspace, scope, context references, environment references, IDs, raw metadata, and usage.
- [ ] Run `GOCACHE=/tmp/agentplane-go-cache go test ./internal/agent/...`; expect the conformance tests to pass.

### Task 4: Pass scheduler-owned correlation to compatible executors

- [ ] Add a failing runtime test with one legacy executor and one executor implementing the optional correlated interface. Assert both execute, and the correlated executor receives exactly the IDs present in `RunResult` and emitted events; `AttemptID` remains empty.
- [ ] Add the typed identity and optional interface in `internal/runtime/scheduler.go`, with one dispatch point inside the worker. Preserve the existing `Executor` signature and Shell path.
- [ ] Run `GOCACHE=/tmp/agentplane-go-cache go test ./internal/runtime ./internal/shell` and `GOCACHE=/tmp/agentplane-go-cache go test -race ./internal/runtime`.

### Task 5: Execute Agent nodes through explicit lookup

- [ ] Add failing tests for valid config, bound or literal task, task conflict, bad types, unknown adapter, unavailable adapter, detection failure, unsupported capability, execution/start failure, cancellation, and unsupported/mismatched output ports.
- [ ] Implement a caller-owned agent executor that decodes only `type: agent`, validates configuration before execution, looks up an adapter, detects capabilities, and passes typed input and correlation to `Execute`.
- [ ] Map normalized output only to declared graph ports. Preserve distinction between absent and zero usage. Ensure errors keep node and adapter identity while redacting task/environment values.
- [ ] Run `GOCACHE=/tmp/agentplane-go-cache go test ./internal/agent/...`.

### Task 6: Integrate without changing Phase 0 behavior

- [ ] Add a runtime integration test using a small test composite executor that routes an Agent node to the mock-backed agent executor and a downstream test node to a deterministic executor. Prove explicit graph binding of the Agent summary; assert lifecycle IDs/events and accepted outputs.
- [ ] Add an integration failure test proving an Agent error skips downstream while an already accepted sibling result remains available in the run result.
- [ ] Run `GOCACHE=/tmp/agentplane-go-cache go test ./internal/runtime ./internal/agent/... ./internal/shell ./cmd/agentplane`.
- [ ] Run `GOCACHE=/tmp/agentplane-go-race-cache go test -race ./...` and `GOCACHE=/tmp/agentplane-go-cache make check`.
- [ ] Inspect `git diff --stat`, `git diff`, and `git status --short`; verify only F1.1 files changed, the user-owned spec was preserved, and no generated files are included.

## Contracts and limits

The graph definition gains an Agent node convention inside the existing opaque `config` map; no top-level schema field or version bump is proposed. The runtime gains a compatible optional executor interface, not a new scheduler state. Event schema v1 retains its existing lifecycle payloads and correlation; attempts remain absent until F1.6. Agent output becomes graph data only through declared ports. CLI execution remains Shell-only in F1.1, because the only F1.1 adapter is test-only.

The main implementation risk is that a real F1.2 CLI may reveal a missing process-level capability. Keep the adapter interface narrow and use F1.2 to justify any addition. The current in-memory result and log retention are unbounded; F1.3 owns streaming/backpressure and F3.x owns durable artifacts. `allowed_paths` cannot prevent writes in F1.1, so user-facing documentation must call it an intended scope rather than enforcement.
