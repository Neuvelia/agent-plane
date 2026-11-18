# Codebase map for development

This document maps the current checkout to the contracts used when changing
AgentPlane. It is for contributors and coding agents; it does not define a new
product feature. Read the relevant feature spec and ADR before changing a
durable contract.

## Where a change belongs

| Area | Owns | Start here |
| --- | --- | --- |
| `internal/domain/graph` | Graph definition types, semantic validation, bindings, value types and graph versioning | `definition.go`, `validation.go`, `bindings.go` |
| `internal/domain/events` | Typed event payloads, schema compatibility, ordering and delivery | `event.go`, `delivery.go` |
| `internal/domain/run` | Run and node states with legal transitions, results, approval requests, correlation and feedback values | `state.go`, `result.go` |
| `internal/adapter/yamlgraph` | YAML parsing of graph definitions into the domain types | `yamlgraph.go`, `dto.go` |
| `internal/adapter/yamlgraph/edit` | The graph editor engine: in-memory, fidelity-preserving YAML operations (ADR-0020), served by the API | `apply.go`, `ops.go`, `doc.go` |
| `internal/adapter/system` | Wall clock and random run identifiers behind engine ports | `system.go` |
| `internal/app/trace` | Event collector that retains lifecycle, evidence and bounded logs | `collector.go` |
| `internal/archtest` | Test that enforces the layer dependency rules over production imports | `archtest_test.go`, `baseline.txt` |
| `internal/fields` | Field descriptor types for node configuration; each validator owns and exports its descriptors (`ConfigFields`, `OutputPorts`) | `fields.go`, `fieldstest/` |
| `internal/runtime` | Run and node states, scheduling, bounded attempts, acceptance, observation, human decisions and opt-in value recording | `scheduler.go`, `loop.go`, `unit.go`, `values.go` |
| `internal/agent` | Provider-neutral Agent node execution contract and adapter selection | `contract.go`, `executor.go` |
| `internal/agent/codex` | Installed Codex CLI process and result/progress translation | `adapter.go` |
| `internal/shell` | Shell node process execution and configuration | `executor.go` |
| `internal/gate` | Deterministic, read-only checks | `executor.go`, `config.go` |
| `internal/gitlocate` | Resolution of the Git binary shared by the engine and the capability report | `gitlocate.go` |
| `internal/gitstate` | Hardened Git runner, Git-observed workspace baselines and changes | `run.go`, `snapshot.go` |
| `internal/capability` | F2.11 read-only capability report: Git, agent adapters and agent CLIs, with bounded probes (ADR-0025) | `capability.go` |
| `internal/worktree` | F3.1 linked worktrees: preflight, create, snapshot, `merge-tree` integration, removal (behind `runtime.WorkspaceManager`, composed by the CLI) | `preflight.go`, `worktree.go`, `integrate.go` |
| `internal/store` | Local append-only JSONL run history, run lock, summary snapshot, retention and prune (uses `golang.org/x/sys` for file locks) | `store.go`, `read.go`, `list.go`, `snapshot.go`, `prune.go`, `lock*.go`, `format.go` |
| `internal/api` | Authenticated loopback API: runs, approvals, the graph editor routes (`/graphs/validate`, `/graphs/grants`, `/graphs/open`, `/graphs/stat`, `/graphs/edit`, `/graphs/save`) and the capability report | `server.go`, `docs.go`, `capabilities.go`, `hub.go` |
| `internal/version` | Build and runtime version metadata | `version.go` |
| `cmd/agentplane` | CLI commands and composition of the engine, adapters, store and API | `main.go`, `cli.go`, `serve.go` |
| `desktop/electron` | Sidecar lifecycle, local API token, IPC allowlist and narrow preload bridge | `main.cjs`, `preload.cjs` |
| `desktop/src` | React presentation, typed bridge calls and event-derived run views; the welcome screen and capability panel (`welcome.tsx`, `capabilities.ts`) | `api.ts`, `reducer.ts`, `App.tsx` |
| `desktop/src/editor` | Graph editor: canvas, YAML view, Issues panel, node inspector and template picker, over the graph document API | `EditorScreen.tsx`, `useGraphDocument.ts`, `api.ts` |

Current direct Go imports within this repository (`go list`, production
packages only):

| Importer | In-repository imports |
| --- | --- |
| `fields`, `gitlocate`, `version` | none |
| `domain/graph` | `fields` |
| `domain/run` | `domain/events`, `domain/graph` |
| `adapter/yamlgraph` | `domain/graph` |
| `adapter/yamlgraph/edit` | `adapter/yamlgraph`, `domain/graph`, `fields` |
| `adapter/system`, `app/trace`, `store` | `domain/events` |
| `gitstate` | `gitlocate` |
| `worktree` | `gitstate` |
| `capability` | `agent`, `gitlocate` |
| `runtime` | `app/trace`, `domain/events`, `domain/graph`, `domain/run`, `gitlocate`, `gitstate` |
| `agent` | `domain/events`, `domain/graph`, `domain/run`, `fields`, `runtime` |
| `agent/codex` | `agent`, `domain/events`, `gitstate` |
| `shell` | `domain/graph`, `fields` |
| `gate` | `domain/events`, `domain/graph`, `fields`, `gitstate`, `runtime` |
| `api` | `adapter/yamlgraph`, `adapter/yamlgraph/edit`, `capability`, `domain/events`, `domain/graph`, `domain/run`, `fields`, `runtime`, `store` |
| `cmd/agentplane` | composition of the Go packages above |

Keep the core direction: `domain/*`, `gitstate` and `gitlocate` do not import the
runtime (the architecture test enforces the layers); the runtime does not import concrete `agent`, `gate`, `shell`, `api`
or `store` packages. Executor interfaces belong to the runtime because it
consumes them; concrete executors implement them. The API uses runtime
contracts but does not own scheduling. `graph/edit` applies operations in
memory (`adapter/yamlgraph/edit`); the API owns granted file saves. The desktop renderer calls the narrow
preload bridge; Electron main owns the sidecar and loopback API token. The
desktop is a client of that API, not a second execution engine.

## Contracts to preserve during a behavior-neutral refactor

1. **Graph:** parsing and validation remain deterministic. Keep schema-v0
   semantics, diagnostic codes/locations/order, binding resolution and
   semantic hashes stable. See `docs/graph-spec.md` and ADR-0001–0003.
2. **Runtime:** the coordinator owns state and accepted values. Preserve
   readiness from bound inputs, bounded retries, accepted sibling values,
   terminal states and cancellation. Shell and Agent work may execute
   concurrently only under their existing rules. See ADR-0004, ADR-0014,
   ADR-0015, ADR-0017 and ADR-0018.
3. **Events:** keep emitted facts, sequence order, correlation IDs, bounded
   progress, redaction and decoding of older schemas stable. No transition
   may become visible only in a log. Recorded run values stay opt-in,
   redacted before truncation and bounded (ADR-0023). See
   `docs/domain-model.md` and the event-related ADRs.
4. **Process boundaries:** retain explicit argv/environment handling,
   termination on cancellation, bounded observations and secret redaction.
   Git-observed scope checks detect changes after execution; they are not
   filesystem isolation. See ADR-0005, ADR-0010 and ADR-0016.
5. **Local clients and history:** preserve CLI exit/JSON contracts, the
   store's append-only event log, the loopback-only authenticated API and
   the desktop's event-derived state. The renderer must not receive the API
   token or gain process/filesystem access. See ADR-0007 and ADR-0019.

Split behavior changes, public contract changes and migrations into their own
feature PR with a spec and, where required by `AGENTS.md`, an ADR. A refactor
PR should identify which contracts it touches, add a focused regression test
only where existing coverage leaves a real gap, run the narrow checks, then
run `make check` and inspect the complete diff. Desktop changes also require
the checks documented in `desktop/README.md`.

## Development harness versus product runtime

`AGENTS.md`, `prompts/`, `scripts/` and `.github/` guide contributors and
coding agents working on this repository. They are separate from the
AgentPlane graph/runtime product under `internal/`. A developer's Git
worktree is not the product's workspace isolation (F3.1, `internal/worktree`), and
neither worktrees nor instructions are a security sandbox for untrusted code.
