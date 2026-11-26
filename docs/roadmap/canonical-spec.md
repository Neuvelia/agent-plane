# Canonical specification

**Status:** accepted (owner decisions 2026-11-26)
**Decisions:** ADR-0032 to ADR-0038, ADR-0027 (index: `decisions/README.md`)

This file resolves the contradictions between the two source documents and
the repository:

- **the roadmap:** `docs/reference/full-roadmap-features.md`, a verified copy of
  `agent-control-plane-roadmap-features.pdf`;
- **the architecture plan:** `docs/reference/full-architecture-plan.md`, a
  verified copy of `agent-control-plane-architektura-plan.pdf`.

**Precedence.** This file overrides both source documents wherever it decides
something. The roadmap decides phase scope, priorities and acceptance criteria
(ADR-0036). The architecture plan is background only. Feature specs in
`docs/features/` define the executable contract of one feature and must not
contradict this file. Changing anything here needs an ADR.

## 1. CLI

The binary is **`agentplane`** (ADR-0032). The architecture plan's `agentctl`
is superseded.

```text
agentplane graph validate PATH [--json]
agentplane run PATH [--input key=value]... [--approve NODE]... [--reject NODE]... [--store-dir DIR] [--watch | --json] ...
agentplane serve [--store-dir DIR] ...      # loopback API for the desktop
agentplane runs list|show|prune ...
agentplane version
```

`cmd/agentplane/cli.go` holds the full usage string. Exit codes:

| Code | Meaning |
| --- | --- |
| 0 | success |
| 2 | usage error |
| 3 | invalid graph |
| 4 | run failure |

## 2. State model (ADR-0032)

### 2.1 Domain states (written by the runtime, `internal/domain/run/state.go`)

| Level | States | Terminal |
| --- | --- | --- |
| Node | `pending`, `ready`, `running`, `succeeded`, `failed`, `skipped`, `cancelled` | `succeeded`, `failed`, `skipped`, `cancelled` |
| Run | `running`, `succeeded`, `failed`, `cancelled` | all except `running` |

- The spelling is `cancelled`.
- An exhausted correction loop leaves the node `failed`, with failure kind
  `needs_intervention`.
- `queued` does not exist until a feature adds queued runs through an ADR.

### 2.2 Derived statuses (computed by readers, never written as events)

| Status | Derived from |
| --- | --- |
| `retrying` | a node with a finished red attempt and a newer attempt started |
| `waiting_human` | `approval_requested` without `approval_decided` |
| `interrupted` (run or node) | no terminal event and no live run lock (F3.2, ADR-0027) |

### 2.3 Mapping of source-document terms

| Source term | Canonical |
| --- | --- |
| roadmap F1.6 `failed_needs_intervention` | node `failed` + failure kind `needs_intervention` |
| roadmap F2.5 `waiting` | `pending` |
| roadmap F2.5 `green` / `red` | `succeeded` / `failed` (a red attempt is an `attempt_finished` fact) |
| roadmap F2.5 `human_waiting` | derived `waiting_human` |
| architecture `PENDING`, `READY`, `RUNNING`, `FAILED` | lowercase domain states |
| architecture `CANCELED` | `cancelled` |
| architecture `CHECKING` | phase: `gate_check` events inside an attempt |
| architecture `CORRECTING` | phase: a new `attempt_started` after a red check |
| architecture `WAITING_FOR_HUMAN` (node/run) | derived `waiting_human` |
| architecture run `QUEUED` | not used (see 2.1) |
| architecture run `INTERRUPTED` | derived `interrupted` (2.2) |

## 3. Event catalog (ADR-0032, snake_case)

Every event carries:

- `event_schema_version` (currently **14**), `type`, timestamp and `sequence`
  (monotonic per run);
- `run_id`, `node_run_id` and `attempt_id` where relevant;
- `graph_version`;
- a typed payload.

Readers accept every earlier schema and reject newer ones. The code
(`internal/domain/events`) is the source of truth; this table must follow it.

| Type | First schema | Feature / ADR | Architecture-plan name |
| --- | --- | --- | --- |
| `run_started` | 1 | F0.6 / ADR-0006 | `RunStarted` |
| `node_ready` | 1 | F0.6 | `NodeReady` |
| `node_started` | 1 | F0.6 | `NodeStarted` |
| `log_chunk` | 1 | F0.6 | (`AgentOutputChunk` for Shell output) |
| `node_finished` | 1 | F0.6 | `NodeSucceeded` and failures |
| `node_terminated` | 1 | F0.6 | none |
| `run_finished` | 1 | F0.6 | `RunSucceeded` and failures |
| `node_evidence` | 2 | F1.2 / ADR-0010 | `ArtifactCreated` (partly) |
| `text_delta`, `tool_call_started`, `tool_call_finished`, `command_started`, `command_finished`, `file_changed`, `status`, `progress_omitted` | 3 | F1.3 / ADR-0011 | `AgentOutputChunk` |
| `gate_check` | 4 (full check result 5) | F1.4, F1.5 / ADR-0012, ADR-0013 | `CheckStarted` + `CheckFailed` (verdict `red`) |
| `attempt_started`, `attempt_input`, `attempt_finished` | 6 | F1.6 / ADR-0014 | `CorrectionRequested` (a new attempt after red) |
| `return_envelope`, `declared_scope_conflicts` | 7 (changed-files envelope 8) | F1.7, F1.8 / ADR-0015, ADR-0016 | none |
| `approval_requested`, `approval_decided` | 9 | F2.10 / ADR-0017 | `HumanApprovalRequested`, `HumanApproved` |
| `value_recorded` | 10 | F2.3-E6 / ADR-0023 | none |
| `workspace_prepared`, `workspace_integrated`, `workspace_conflict`, `workspace_released`, `source_workspace_changed` | 11 | F3.1 / ADR-0024 | none |
| `attempt_diff` | 12 | F2.8b / ADR-0026 | none |
| `cancel_requested`, `process_terminated` | 13 | F3.4 / ADR-0028 (defined, not yet emitted) | none |
| `policy_evaluated`, `policy_violation` | 14 | F3.8, F3.9 / ADR-0029 (defined, not yet emitted) | none |

The architecture plan's `ArtifactCreated` has no canonical event yet. It
arrives with F3.5 (artifact store).

## 4. Graph format (ADR-0033)

`docs/graph-spec.md` is the full contract. The essentials:

- **Fields:** `schema_version` (0), `id`, `name`, `metadata`, `inputs`, `nodes`,
  `edges`, `graph_input_bindings`, `outputs`, plus optional `units`, `secrets`,
  `limits` and `layout`. Unknown fields are errors.
- **Edges** bind a concrete source output to a concrete target input
  (`from: a.outputs.x`, `to: b.inputs.y`). No ordering-only edges, no
  `depends_on`, no `foreach`, no `when`. `{{ }}` is never interpolated.
- **Graph version** is the computed semantic hash `graph_version`. Layout
  metadata does not change it. There is no field for an author-written version.
- **JSON** files are accepted through the YAML parser; a contract test is
  pending (#179).
- **Node types:** `shell`, `agent`, `gate`, `human`. An unknown type must be a
  validation error; this is not implemented yet (#178).

Example. `agentplane graph validate` passes it with no diagnostics, and a run
with `--input topic=x` outputs `left:x+right`:

```yaml
schema_version: 0
id: canonical-example
name: Canonical example

metadata:
  description: Two independent branches joined by an explicit data binding.

inputs:
  topic:
    type: string
    required: true

nodes:
  left:
    type: shell
    inputs:
      topic: {type: string, required: true}
    config:
      command: /bin/sh
      args: ["-c", 'printf "left:%s" "$TOPIC"']
      input_env: {topic: TOPIC}
    outputs:
      stdout: {type: string, required: true}

  right:
    type: shell
    config:
      command: /usr/bin/printf
      args: ["right"]
    outputs:
      stdout: {type: string, required: true}

  join:
    type: shell
    inputs:
      left_text: {type: string, required: true}
      right_text: {type: string, required: true}
    config:
      command: /bin/sh
      args: ["-c", 'printf "%s+%s" "$LEFT" "$RIGHT"']
      input_env: {left_text: LEFT, right_text: RIGHT}
    outputs:
      stdout: {type: string, required: true}

edges:
  - {from: left.outputs.stdout, to: join.inputs.left_text}
  - {from: right.outputs.stdout, to: join.inputs.right_text}

graph_input_bindings:
  - {from: inputs.topic, to: left.inputs.topic}

outputs:
  result:
    from: join.outputs.stdout
```

The architecture plan §11 example (`version: 1`, `depends_on`, `foreach`,
templating) is an outdated sketch. Dynamic fan-out is the future entry `FX.1`.

## 5. Agent adapter interface (ADR-0034, ADR-0009)

```go
type AgentAdapter interface {
    ID() AdapterID
    Detect(context.Context) (CapabilityReport, error) // tri-state capabilities
    Execute(context.Context, AgentExecutionInput) (AgentExecutionOutput, error)
}
// Optional: ExecuteWithNormalizedProgress for streaming (F1.3).
```

| Roadmap / architecture operation | Canonical form |
| --- | --- |
| detect capabilities | `Detect` |
| execute | `Execute` |
| stream events | the optional progress interface |
| cancel | `ctx` cancellation |
| collect outputs | the `Execute` result: summary, changed files, artifacts, raw metadata, usage |
| prepare | not present; adding it needs an ADR |

Unknown usage is `nil`, never `0`.

## 6. Units, retry and blast radius (ADR-0036, ADR-0037)

- **Units** are static loop nodes (`unit_id = node_id`, ADR-0015). Retry
  touches only the red unit. Dynamic fan-out is the future entry `FX.1`.
- **Test commands as checks** are a gap to close: feature `F1.4b`, issue #183.
  Gates stay read-only (ADR-0012) until F1.4b amends that.
- **Blast radius:** the consequence model (L1–L3) belongs to F3.9. The F2.10
  panel context waits for it (#187).

## 7. Storage, stack and layout (ADR-0027, ADR-0035)

- **Run history:** a JSONL event log per run, a rebuildable `summary.json` and
  `store.json` retention. No SQLite.
- **Stack:** Go runtime and CLI; Electron + React desktop. No Tauri.
- **Layout:** `cmd/` + `internal/`. The roadmap §10 package names are
  ownership areas (`owner_package` in `features.yaml`):

| `owner_package` | Directories |
| --- | --- |
| `graph-schema` | `internal/domain/graph`, `internal/adapter/yamlgraph`, `internal/fields` |
| `execution-core` | `internal/runtime`, `internal/app`, `internal/domain/run` |
| `event-model` | `internal/domain/events` |
| `adapters` | `internal/shell`, `internal/agent`, `internal/gate`, `internal/capability` |
| `workspace-manager` | `internal/worktree`, `internal/gitstate`, `internal/proctree`, `internal/adapter/envsecrets`, `internal/adapter/secretscan`, `internal/domain/secrets` |
| `local-store` | `internal/store` |
| `runner-daemon` | `internal/api`, `cmd/agentplane` (`serve`) |
| `cli` | `cmd/agentplane` |
| `ui` | `desktop/` |
| `cloud-api` | none yet (Phase 4+) |
| `release` | `scripts/`, `.github/`, packaging (REL entries) |

## 8. Invariants

These come from roadmap §2 and AGENTS.md and are checked at every re-audit:

1. **Local-first:** no mandatory remote service; listeners bind to loopback only.
2. **The graph is the contract:** a graph file runs without the UI, and the UI
   edits the file.
3. **Node ≠ agent:** no LLM for deterministic transformations.
4. **Retry the smallest unit:** accepted siblings are never rerun.
5. **Evidence before confidence:** a gate uses tests, exit codes or scope, not
   model judgment.
6. **Human gate by blast radius:** the model belongs to F3.9.
7. **Trace is the product:** every state transition emits an event, and every
   red verdict has a reason and evidence.
8. **The cloud runs no arbitrary command:** no endpoint executes arbitrary
   commands outside a graph run.
9. **No secrets** in graphs, events, logs or exports.
10. **Unknown is not zero** in any metric.
