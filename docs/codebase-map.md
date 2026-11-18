# Codebase map for development

This document maps the current checkout to the contracts used when changing
AgentPlane. It is for contributors and coding agents; it does not define a new
product feature. Read the relevant feature spec and ADR before changing a
durable contract.

## Where a change belongs

| Area | Owns | Start here |
| --- | --- | --- |
| `internal/graph` | YAML definitions, semantic validation, bindings, value types and graph versioning | `definition.go`, `validation.go`, `bindings.go` |
| `internal/graph/edit` | In-memory, fidelity-preserving graph YAML operations | `apply.go`, `ops.go`, `doc.go` |
| `internal/fields` | Field descriptor types for node configuration; each validator owns and exports its descriptors (`ConfigFields`, `OutputPorts`) | `fields.go`, `fieldstest/` |
| `internal/events` | Typed event payloads, schema compatibility, ordering and collection | `event.go`, `emitter.go` |
| `internal/runtime` | Run and node states, scheduling, bounded attempts, acceptance, observation, human decisions and opt-in value recording | `scheduler.go`, `loop.go`, `unit.go`, `values.go` |
| `internal/agent` | Provider-neutral Agent node execution contract and adapter selection | `contract.go`, `executor.go` |
| `internal/agent/codex` | Installed Codex CLI process and result/progress translation | `adapter.go` |
| `internal/shell` | Shell node process execution and configuration | `executor.go` |
| `internal/gate` | Deterministic, read-only checks | `executor.go`, `config.go` |
| `internal/proctree` | Neutral process-tree ownership: start a child in its own Unix process group, terminate by request, grace, force, verify (ADR-0028; Windows is an interim direct-process stub until F3.4a) | `proctree.go`, `proctree_unix.go`, `policy.go` |
| `internal/gitstate` | Hardened Git runner, Git-observed workspace baselines and changes | `run.go`, `snapshot.go` |
| `internal/worktree` | F3.1 linked worktrees: preflight, create, snapshot, `merge-tree` integration, removal (behind `runtime.WorkspaceManager`, composed by the CLI) | `preflight.go`, `worktree.go`, `integrate.go` |
| `internal/store` | Local append-only JSONL run history | `store.go` |
| `internal/api` | Authenticated loopback API for runs, approvals and granted graph documents | `server.go`, `docs.go`, `hub.go` |
| `internal/version` | Build and runtime version metadata | `version.go` |
| `cmd/agentplane` | CLI commands and composition of the engine, adapters, store and API | `main.go`, `cli.go`, `serve.go` |
| `desktop/electron` | Sidecar lifecycle, local API token, IPC allowlist and narrow preload bridge | `main.cjs`, `preload.cjs` |
| `desktop/src` | React presentation, typed bridge calls and event-derived run views | `api.ts`, `reducer.ts`, `App.tsx` |

Current direct Go imports within this repository (`go list`, production
packages only):

| Importer | In-repository imports |
| --- | --- |
| `fields`, `events`, `gitstate`, `version` | none |
| `graph` | `fields` |
| `worktree` | `gitstate` |
| `graph/edit` | `graph` |
| `runtime` | `events`, `gitstate`, `graph` |
| `agent` | `events`, `fields`, `graph`, `runtime` |
| `agent/codex` | `agent`, `events`, `gitstate` |
| `shell` | `fields`, `graph` |
| `gate` | `events`, `fields`, `gitstate`, `graph`, `runtime` |
| `store` | `events` |
| `api` | `events`, `graph`, `graph/edit`, `runtime`, `store` |
| `cmd/agentplane` | composition of the Go packages above |

Keep the core direction: `graph`, `events` and `gitstate` do not import the
runtime; the runtime does not import concrete `agent`, `gate`, `shell`, `api`
or `store` packages. Executor interfaces belong to the runtime because it
consumes them; concrete executors implement them. The API uses runtime
contracts but does not own scheduling. `graph/edit` applies operations in
memory; the API owns granted file saves. The desktop renderer calls the narrow
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
