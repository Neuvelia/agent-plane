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
pending -> ready -> running -> succeeded|failed|cancelled|skipped
```

State transitions must be represented by structured events.

## Events

The runtime uses append-oriented execution events so the execution path can be reconstructed.

Minimum early events:

```text
run_started
node_ready
node_started
log_chunk
node_finished
run_finished
```

Later events include check, correction, artifact, and approval events.

The event stream is an observable record of execution, not a container for hidden model reasoning.

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
