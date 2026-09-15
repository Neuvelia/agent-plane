# AgentPlane

AgentPlane is a local-first execution and control plane for AI coding agents.

The project focuses on orchestration, deterministic validation, bounded correction loops, execution traces, and human control. It is intentionally not another agent framework.

## Status

Early repository scaffold. The active milestone is **Phase 0 - Execution Core**.

The first target is a CLI-driven graph runtime that can execute independent Shell nodes concurrently, pass explicit data between nodes, emit structured events, and finish deterministically.

## Start here

1. Read `AGENTS.md`.
2. Read `docs/architecture.md` and `docs/graph-spec.md`.
3. Read `docs/features/F0.1-graph-definition-schema-v0.md`.
4. Create a branch or worktree for F0.1.
5. Use `prompts/plan.md` with Codex before implementation.
6. Implement the approved plan.
7. Run `make check`.
8. Use `prompts/review.md` in a separate Codex session.

## Repository layout

```text
AGENTS.md                  Durable instructions for coding agents
cmd/agentplane/            CLI entrypoint
internal/                  Runtime implementation packages
.agents/graphs/            Example AgentPlane graph definitions
docs/architecture.md       Product/runtime architecture contract
docs/domain-model.md       Core entities and invariants
docs/graph-spec.md         Graph Definition Schema v0 draft
docs/features/             Feature specifications and acceptance criteria
docs/adr/                  Architectural decision records
prompts/                    Codex workflow prompts
experiments/                Measurement data for development/thesis analysis
scripts/                    Local developer commands
```

## Local commands

```bash
make fmt
make test
make check
./scripts/new-feature.sh F0.3 data-bindings
./scripts/new-adr.sh event-store-choice
```

Build the CLI:

```bash
go build -o bin/agentplane ./cmd/agentplane
./bin/agentplane version
```

## Current milestone

Phase 0 features are tracked in `TASKS.md` and specified under `docs/features/`.

Do not start the visual IDE, cloud control plane, billing, or managed execution before the runtime contracts are proven.

## Working with Codex

Recommended flow:

```text
feature spec
    -> Codex plan
    -> human plan approval
    -> isolated branch/worktree
    -> Codex implementation
    -> deterministic checks
    -> separate Codex review
    -> human review
    -> merge
```

See `docs/development-workflow.md` for the full process.
