# AGENTS.md

## Project mission

AgentPlane is a local-first execution and control plane for AI coding agents.

The product is not an agent framework and not a generic workflow builder. It orchestrates existing agents and deterministic tools, records their execution, validates outputs, retries the smallest failed unit, and exposes evidence for debugging and human approval.

The core model is:

- A graph defines what work exists and which units depend on which outputs.
- A loop lives inside a node and performs produce -> check -> correct until green or until the retry budget is exhausted.
- A node is not necessarily an LLM. Shell, code, gate, and human nodes are first-class.
- An edge represents a real data dependency, not merely "and then" ordering.

Read these files before making architectural changes:

- `docs/architecture.md`
- `docs/domain-model.md`
- `docs/graph-spec.md`
- `docs/development-workflow.md`
- the relevant file under `docs/features/`
- relevant ADRs under `docs/adr/`

## Current product phase

Phase 0: Execution Core is implemented on `main` through F0.7. F1.1 Agent
Adapter Contract and F1.2 installed-Codex execution are implemented, including
Git-derived evidence and a verified real CLI smoke. F1.3 adds bounded normalized
progress and live delivery (ADR-0011). F1.4 adds deterministic read-only Gates
(ADR-0012). Later Phase 1 Agent Loop features remain unimplemented; F1.5 is
the next planning candidate. Read the
active feature spec before changing execution contracts; do not treat the roadmap as an executable spec.

The Phase 0 objective was to prove graph/node/edge/run semantics without
building the UI or cloud control plane.

Phase 0 scope:

1. Graph Definition Schema v0.
2. Graph Validator and lint.
3. Explicit data bindings on edges.
4. In-memory DAG scheduler.
5. Shell Node.
6. Event Model with stable run/node/attempt identifiers.
7. CLI runner.

Phase 0 exit criterion, covered by the CLI integration test:

A graph with three Shell nodes can run two independent branches concurrently, pass data to a dependent node, emit a reconstructable event stream, and terminate with a deterministic result.

Implement only the active feature scope. Do not add later-phase behavior unless
its spec explicitly requires a compatibility seam.

## Non-negotiable architecture rules

### 1. Local-first

Core graph authoring, execution, loops, gates, history, and debugging must not require a cloud account.

Do not introduce mandatory remote services into the execution engine.

### 2. Graph is the execution contract

The graph definition is a text file that can be stored in Git and executed without the visual editor.

The future UI is an editor of this representation, not the only source of truth.

### 3. Node != agent

Do not use an LLM for deterministic transformations that can be implemented as code.

Prefer deterministic code for parsing, merging, sorting, filtering, schema validation, path checks, and exact comparisons.

### 4. Edges represent data dependencies

An edge must identify what output of the source becomes what input of the target.

Do not create ordering-only edges unless the graph specification explicitly supports a separate control dependency type.

### 5. Deterministic evidence before model judgment

If correctness can be checked using an exit code, test, type check, schema, file scope, or exact predicate, use that before any LLM-based assessment.

A model saying that its output "looks correct" is not a gate.

### 6. Retry the smallest failed unit

A failed unit must not cause accepted sibling units to be rerun unless the graph explicitly requests replanning.

Accepted output is immutable for the current run unless an explicit graph transition invalidates it.

### 7. Retry is bounded

Loops must have a maximum number of attempts. The default target for later agent loops is 3.

Do not implement unbounded retry.

### 8. Trace is part of correctness

Every execution transition must be observable through structured events.

Do not add execution paths that are only visible in ad-hoc logs.

### 9. Human approval is based on consequence

Later human gates should be driven by blast radius and reversibility, not by self-reported model confidence.

### 10. Execution code is untrusted input territory

Agent output, shell commands, graph definitions, paths, environment values, and future remote messages must be treated as untrusted inputs.

Do not log secrets. Do not silently widen filesystem or process permissions.

## Repository boundaries

Expected high-level layout:

```text
cmd/agentplane/          CLI entrypoint
internal/graph/          graph schema, parsing, validation, bindings
internal/runtime/        scheduler and execution state transitions
internal/events/         event contracts and event emission
internal/version/        build/runtime version metadata
.agents/graphs/          example graph definitions
docs/                    architecture and product contracts
docs/features/           executable feature specifications
docs/adr/                architectural decision records
prompts/                 reusable prompts for Codex workflow
experiments/             thesis/development measurement data
scripts/                 developer automation
```

The future UI and cloud backend should be added as separate top-level components. Do not put cloud concerns inside `internal/runtime`.

## Development workflow for every feature

### Step 1: Read before editing

Read:

1. this file;
2. the feature specification;
3. relevant architecture/specification docs;
4. relevant code and tests;
5. relevant ADRs.

Do not start by writing code.

### Step 2: Produce a plan

Before implementation, report:

1. current state;
2. proposed change;
3. files to modify;
4. tests to add or update;
5. state/data contracts affected;
6. risks and edge cases;
7. ambiguities in the feature specification.

If the requested task is implementation and the user has already approved the plan, continue. Otherwise stop after planning when explicitly asked for plan-only work.

### Step 3: Implement only the active scope

Use the feature ID from `docs/features/` as the scope boundary.

Do not opportunistically refactor unrelated code.

Do not implement a later roadmap feature because it appears convenient.

If a small prerequisite is missing, either implement the minimum seam or document it as a blocker.

### Step 4: Run deterministic checks

At minimum run:

```bash
make check
```

If the changed component has narrower tests, run those first, then the full check.

Never claim success without reporting the actual check result.

### Step 5: Review the diff

Before finishing:

- inspect `git diff --stat`;
- inspect changed files;
- confirm no unrelated files changed;
- confirm tests exercise acceptance criteria;
- confirm docs/specs remain consistent;
- confirm generated or temporary files are not committed.

### Step 6: Report completion

Return:

1. summary of behavior implemented;
2. files changed;
3. checks executed and results;
4. known limitations;
5. follow-up work explicitly outside the feature scope.

## Definition of done

A feature is not done merely because the code compiles.

A feature is done when:

- every acceptance criterion in its feature spec is covered;
- behavior is tested at the appropriate level;
- failure behavior is tested, not only the happy path;
- deterministic checks pass;
- public contracts are documented;
- no unrelated scope expansion is present;
- execution-affecting behavior emits structured events where applicable;
- new architectural decisions have an ADR when required.

## Testing principles

Tests should prove contracts rather than implementation details.

Prioritize:

1. schema and validation tests;
2. state transition tests;
3. scheduler ordering/concurrency tests;
4. process lifecycle tests;
5. failure and cancellation tests;
6. serialization round-trip tests;
7. integration tests using temporary repositories/directories.

Avoid sleeps as synchronization when a deterministic channel, barrier, or fake clock can be used.

When testing concurrency, prove overlap or ordering using explicit synchronization rather than timing assumptions wherever possible.

## Error handling

Errors should be actionable and structured enough for both CLI and future UI usage.

Prefer errors that answer:

- what failed;
- which graph/node/binding caused it;
- where in the source definition it came from when available;
- how the user can correct it.

Do not panic for expected user/configuration/process failures.

## Event rules

Execution events are append-oriented facts, not mutable status blobs.

Each event should eventually be able to carry:

- event type;
- timestamp;
- sequence number within a run;
- graph run ID;
- node run ID when relevant;
- attempt ID when relevant;
- graph version/hash;
- typed payload.

Never place raw secrets in events.

Do not expose private model reasoning. Store observable actions, tool/process events, outputs, checks, evidence, and metadata instead.

## Graph rules

Graph definitions must be deterministic to parse and validate.

Required design properties:

- explicit `schema_version`;
- stable node IDs;
- explicit input/output contracts;
- explicit data bindings;
- cycle detection;
- no secrets embedded in graph files;
- semantic content separate from visual layout metadata;
- forward migration path for future schema versions.

See `docs/graph-spec.md` for the current draft.

## Security rules

For all process execution code:

- pass arguments as structured argv where possible instead of concatenated shell strings;
- treat cwd and paths as untrusted;
- keep environment injection explicit;
- redact configured secrets from logs/events;
- ensure child processes can be terminated;
- do not expose a network listener beyond loopback by default;
- never send repository content to a remote service implicitly.

If a requested change weakens these defaults, stop and call it out.

## Dependency policy

Prefer the standard library for the Phase 0 runtime unless an external dependency materially reduces risk or complexity.

Before adding a dependency, explain:

- what capability it provides;
- why the standard library is insufficient;
- whether it affects distribution or security;
- whether the dependency is runtime or development-only.

Do not add frameworks preemptively for future features.

## Code style

The Phase 0 runtime is Go.

- Keep packages small and domain-oriented.
- Prefer explicit types over `map[string]any` at stable boundaries.
- Keep interfaces consumer-owned and minimal.
- Avoid global mutable state.
- Use `context.Context` for cancellable operations when execution work is introduced.
- Return errors with useful context while preserving causes.
- Keep serialization types separate from runtime state where that prevents coupling.
- Comments should explain invariants and non-obvious decisions, not restate code.

## Git and commit discipline

One feature should normally map to one branch/worktree.

Suggested branch names:

```text
codex/F0.1-graph-schema
codex/F0.2-graph-validator
codex/F0.4-dag-scheduler
```

Prefer small commits that describe one coherent change.

Examples:

```text
feat(graph): add schema v0 types
feat(graph): validate node references
feat(runtime): schedule independent nodes concurrently
test(runtime): prevent downstream execution before dependencies finish
```

Do not rewrite unrelated history.

## When an ADR is required

Create an ADR when a decision:

- changes a public or durable contract;
- introduces a major dependency or framework;
- chooses a persistence model;
- changes graph semantics;
- changes event semantics;
- changes process/workspace isolation;
- affects local-vs-cloud trust boundaries;
- is expensive to reverse.

Use `docs/adr/0000-template.md`.

## When a feature spec is required

Any roadmap feature or non-trivial behavior change must have a feature spec before implementation.

Use `docs/features/_template.md`.

The spec must include measurable acceptance criteria. Avoid criteria such as "works well", "is robust", or "looks correct".

## Out of scope until explicitly activated

Do not implement these during Phase 0 unless a feature spec explicitly activates them:

- visual graph editor;
- desktop shell;
- cloud control plane;
- accounts/authentication;
- billing;
- team/RBAC features;
- managed cloud runners;
- Kubernetes orchestration;
- agent memory/learning edge;
- marketplace/plugins;
- advanced policy language;
- LLM-as-judge as a safety gate;
- automatic prompt optimization.

## Codex role separation

For important changes, use separate sessions/agents for implementation and review.

Implementer responsibilities:

- follow the active feature spec;
- make the smallest coherent change;
- add tests;
- run checks;
- report limitations.

Reviewer responsibilities:

- do not modify code unless explicitly asked;
- compare the diff to the feature spec and this file;
- look for missing failure cases, race conditions, state errors, scope expansion, unsafe process handling, and missing tests;
- rank findings by severity.

Reusable prompts live in `prompts/`.
