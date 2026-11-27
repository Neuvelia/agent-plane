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

- `docs/codebase-map.md`
- `docs/architecture.md`
- `docs/domain-model.md`
- `docs/graph-spec.md`
- `docs/development-workflow.md`
- the relevant file under `docs/features/`
- relevant ADRs under `docs/adr/`

## Current product phase

As of 2026-11-26: Phase 2 (Local IDE) is being completed and Phase 3 (Local
Production Ready) is in progress; Phases 0 and 1 meet their exit criteria with
open acceptance gaps listed in `docs/roadmap/audit/AUDIT-2026-11-25.md`.

Phase 0: Execution Core is implemented on `main` through F0.7. F1.1 Agent
Adapter Contract and F1.2 installed-Codex execution are implemented, including
Git-derived evidence and a verified real CLI smoke. F1.3 adds bounded normalized
progress and live delivery (ADR-0011). F1.4 adds deterministic read-only Gates
(ADR-0012). F1.5 adds versioned Check Results with typed references,
checked-input scope, duration and typed trace lookup (ADR-0013). F1.6 adds bounded
node-local correction attempts with embedded Gates, typed feedback,
and schema-6 attempt trace (ADR-0014). F1.7 adds explicit static units,
Return Envelopes, immutable accepted runtime values/provenance and conservative
shared-workspace Agent scheduling (ADR-0015). F1.8 adds opt-in Git-observed
changed-files scope checking (ADR-0016; event schema 8). Shared files are not
isolated; F1.8 detects out-of-scope changes after execution and neither prevents
writes nor isolates the workspace by itself. Phase 1 features F1.1-F1.8 are
implemented; the real-Codex F1.7 demo was executed on 2026-11-17 against Codex 0.169.0 (`docs/validation/F1.7-real-codex-demo.md`). F3.1
(ADR-0024, event schema 11) adds opt-in `isolation: worktree` for loop Agent
units: one linked Git worktree per node run, accepted units integrated onto a
run-scoped ref, and a non-fatal `source_workspace_changed` guard. It is not a
sandbox, gives downstream nodes no view of upstream files, does not isolate Shell
nodes, and supports Linux/macOS only; a real-Codex run passed once
(`docs/validation/F3.1-real-codex-demo.md`). F3.2 (ADR-0027) makes a run's durable
state exactly its JSONL event log: persist-before-deliver, durable creation, a run
lock that separates `running` from `interrupted` (derived, no new event), a
rebuildable `summary.json`, `store.json` and explicit `runs prune` retention.
Implemented on Linux; the Windows tests have not been run.
F2.8b adds opt-in `--record-diffs` capture of bounded, redacted per-attempt file diffs for loop Agent
nodes as `attempt_diff` events (event schema 12, ADR-0026); the desktop diff panel is not implemented.
A Phase 2 vertical slice is also implemented (approval events: event schema 9): F1.6a adds the bounded
loop to Shell nodes (ADR-0018), F2.10 adds the `human` node and local approval
events (ADR-0017), F2.9 adds an opt-in JSONL run store with `runs list/show`, and
F2.5 adds the loopback `serve` API plus the `desktop/` Electron shell
(ADR-0019). The desktop UI observes and steers runs through that API; it never
schedules, retries or decides state. F2.5a (presentation only) put the
desktop UI on the Tarmind design system with PL/EN localisation and a dev-only
Storybook; the API and event contracts are unchanged. Slice E6 of F2.3 adds
opt-in, bounded and redacted `value_recorded` events (event schema 10,
ADR-0023 accepted, `--record-values`) and a desktop Settings switch that
starts the sidecar with it; the E5b.1 values reducer and run picker and the E5b.2 edge inspector (the recorded value of a selected edge or graph input binding, shown as text only) are implemented in the renderer. The graph
editor (F2.1, F2.4 Issues panel) and the F2.2 node inspector edit graph files through
versioned operations (ADR-0020, ADR-0021) served by `POST /graphs/open`, `/graphs/stat`,
`/graphs/edit` and `PUT /graphs/save` on granted files (`desktop/src/editor`,
`internal/adapter/yamlgraph/edit`); inspector fields come from
descriptors owned by the validators (`internal/fields`). F2.11 adds the read-only
capability report (`GET /capabilities`, `POST /capabilities/check`, ADR-0025) and the
desktop welcome screen; F2.12 adds bundled graph templates and the template picker.
F3.8 (event schema 14, ADR-0029) adds graph-declared secret references with
the `env` provider for Shell nodes: per-node environment delivery, redaction of
resolved values and an after-the-fact workspace scan. It is not a sandbox, does not
protect a value from the process it is granted to, and leaves Agent `secret_env`, the
keychain provider and policy enforcement to later work (F3.9).
Read the
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
internal/domain/         graph (schema, validation, bindings), events and run vocabulary
internal/adapter/        YAML graph parsing and edit operations, system clock and IDs
internal/app/            application services shared by the engine (trace collector)
internal/archtest/       layer dependency rules enforced as a test
internal/fields/         node configuration field descriptors for editors
internal/runtime/        scheduler and execution state transitions
internal/capability/     read-only capability report (Git, agent adapters, agent CLIs)
internal/gitlocate/      resolution of the Git binary
internal/agent/          provider-neutral Agent contract and adapters
internal/shell/          Shell execution
internal/gate/           deterministic checks
internal/gitstate/       Git-observed workspace state, hardened Git runner
internal/proctree/       process-tree ownership and termination (ADR-0028)
internal/testenv/        test-only platform guards
internal/worktree/       linked Git worktrees for workspace isolation (F3.1)
internal/store/          local run history
internal/api/            loopback API for local clients
internal/version/        build/runtime version metadata
desktop/                 Electron shell and React renderer
.agents/graphs/          example graph definitions
docs/                    architecture and product contracts
docs/features/           executable feature specifications
docs/roadmap/            roadmap registry, canonical spec, status, audits (ADR-0038)
docs/adr/                architectural decision records
prompts/                 reusable prompts for Codex workflow
experiments/             thesis/development measurement data
scripts/                 developer automation
```

The desktop UI is a separate top-level component. A future cloud backend
should also be separate; do not put cloud concerns inside `internal/runtime`.

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

For work with more than a few steps, keep a `TASKS.md` checklist at the
worktree root. Record the objective, branch, spec or plan, completed and next
steps, last verification result, and any blocker. Update it after meaningful
progress and before a handoff or context compaction. On resumption, read it and
check `git status` before acting; the journal may be stale. Keep secrets, raw
model output, and sensitive repository content out of it. `TASKS.md` is ignored
by Git and does not replace the feature spec, implementation plan, or final
report. One-step work does not need a journal.

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

## Roadmap discipline

Feature status lives in `docs/roadmap/features.yaml` (ADR-0038); `docs/roadmap/README.md`
describes the workflow. For every feature, fix or docs change:

- Before work, read the feature's entry in `docs/roadmap/features.yaml`,
  `docs/roadmap/canonical-spec.md` and the linked ADRs. The canonical spec overrides
  the source documents in `docs/reference/` where it decides something.
- Stay inside the entry's `out_of_scope`; widening it needs a new ADR.
- Any decision that changes a spec, a contract or the canonical spec is a new ADR
  (`scripts/new-adr.sh`), never a silent edit. A divergence without an ADR is a gap:
  record it as an open criterion or a divergence with a GitHub issue; every deferral
  names its target feature ID.
- Tag tests that prove a criterion (`// roadmap: F1.7-AC1` above a Go test,
  `[F1.7-AC1]` in a vitest name, a `// roadmap:` comment in the e2e script) and list
  them in the criterion's `evidence`.
- After work, update criterion status/evidence, feature status, `divergences` and
  `tasks`; run `make roadmap` and `make check`; commit `docs/roadmap/STATUS.md`.
- Reference the feature ID in commits and pull requests (`[F1.7] ...`).
- Reusable prompts for planning, implementation, fixes, docs, reviews, new specs and
  re-audits live in `prompts/` (`prompts/README.md`).

Issues and labels (`docs/roadmap/issue-triage.md`):

- Every issue you open gets exactly one class label: `claude-ready` when all seven
  conditions of the triage rule hold (one feature, unambiguous outcome, no ADR,
  deterministic proof, runs in an agent container, no trust boundary, one PR),
  otherwise `needs-owner`. When in doubt, `needs-owner`.
- `needs_decision`, `epic`, `spike` and `feature` are always `needs-owner`; the
  `issue-labels` workflow enforces that and the `triage` label.
- Put the feature ID in the title (`[F2.5b] ...`). A `claude-ready` body states the
  expected outcome and how to verify it; a `needs-owner` body ends with
  `Owner input needed: ...`.
- Take only `claude-ready` issues on your own. If one turns out to need a
  decision, stop, comment what is missing and swap the label to `needs-owner`.

Roadmap invariants (roadmap §2), checked in every change and re-audit:

- [ ] Local-first: no cloud account or remote service is required; listeners stay on loopback.
- [ ] The graph is the contract: graph files run without the UI; the UI edits the file.
- [ ] Node != agent: no LLM for deterministic transformations.
- [ ] Retry the smallest unit: accepted siblings are never rerun.
- [ ] Evidence before confidence: checks use tests, exit codes, scope, schemas.
- [ ] Human gates by blast radius, not model confidence.
- [ ] Trace is the product: every transition emits an event; every red verdict has reason and evidence.
- [ ] The cloud never executes arbitrary commands; no such endpoint locally either.

Roadmap Definition of Done (roadmap §11), in addition to the list below:

- [ ] unit tests for the main semantics;
- [ ] an end-to-end test when the execution path is touched;
- [ ] emits the needed events; the UI never parses incidental text;
- [ ] a readable failure mode and reason;
- [ ] an explicit security/privacy impact;
- [ ] no secrets in the trace;
- [ ] configuration documented;
- [ ] UI works after a refresh from backend/runtime state, not only component state;
- [ ] side effects: behavior during retry/recovery is described;
- [ ] at least one metric or event shows whether it works in real runs;
- [ ] `docs/roadmap/features.yaml` updated and `make roadmap` / `make check` pass.

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

Upgrading the YAML library (`go.yaml.in/yaml/v3`) must include running the `ParseErrorColumn` tests in `internal/adapter/yamlgraph/edit` and `internal/api`: they pin error-message wording the parse-error column lookup depends on.

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

## Clean code rules

These apply to all new and changed Go code in `cmd/` and `internal/`. Thresholds
are mechanical; a breach needs a one-line justification in the code or PR, not
silence. Do not "fix" legacy breaches opportunistically outside the active
feature scope; do not add new ones.

Naming and functions:

- Names reveal intent. No abbreviations except `ctx`, `err`, `id`. Package names
  are short, lowercase, singular and do not stutter (`graph.Graph`, not
  `graph.GraphValidator`). No `util`, `common`, `helpers`, `types`, `base`.
- One level of abstraction per function; it does one thing. Body target <= 30
  lines, hard limit 50.
- At most 4 parameters (excluding `ctx`). No boolean parameters that switch
  behavior; split the function instead.
- Cyclomatic complexity <= 10, cognitive complexity <= 15, nesting depth <= 3.
  Prefer guard clauses and early returns over `else` chains.
- A function either changes state or returns information, not both, except in
  conventional documented cases.
- No output parameters and no named-return tricks that obscure flow.

Types and files:

- Single responsibility: one reason to change per type and per package. File
  target <= 300 lines, hard limit 500. More than 7 exported methods on a type
  needs a justification or a split.
- No `any`/`map[string]any` in domain or application APIs. Use typed IDs and
  typed enums with validating constructors so illegal states are
  unrepresentable.
- Prefer composition. Embed only for genuine interface promotion.
- Remove duplicated knowledge, but do not abstract two similar lines.
- No dead code, unused parameters/fields/exports, commented-out code or stale
  TODOs.

Errors, comments, concurrency:

- No panics for expected failures. Wrap with `%w` and context. Use sentinel or
  typed errors for conditions callers branch on and compare with
  `errors.Is`/`errors.As`, never by string. Never swallow an error and never
  both log and return it.
- Every exported identifier has a doc comment starting with its name. Comments
  state invariants, non-obvious decisions and ADR/spec references.
- Every goroutine has an owner, a `ctx` cancellation path and a bounded
  lifetime. `go test -race` must stay clean.

Tests:

- Tests prove contracts, not implementation. One behavior per test, names
  describe behavior, table-driven where it clarifies. No sleeps for
  synchronization. Use hand-written fakes of ports, not mocking frameworks.
  Test code follows the same naming and duplication rules.

## Clean architecture rules

Layers, inside out: domain -> application -> adapters / delivery -> composition
root (`cmd/agentplane`).

- R1 Dependencies point inward only. Domain imports nothing from this project
  and only the standard library. Application imports domain and never an
  adapter or delivery package.
- R2 Adapters (shell, agent/codex, gitstate, store, graph file/YAML loading)
  implement ports and never import each other or delivery. Cross-adapter needs
  go through a port.
- R3 Ports are small (1-3 methods), owned by the consuming package, named for
  the role (not `IFoo`/`FooImpl`). Accept interfaces, return concrete types.
- R4 Domain and application code do not call `time.Now`, `os`, `os/exec`,
  `net/http`, environment lookups or randomness directly. Inject a clock, ID
  generator, process runner or filesystem port.
- R5 Transport and serialization types (YAML, API payloads, JSONL records) live
  in adapters/delivery and are mapped to domain types. Event schema types are
  the documented exception and stay in the event contract package.
- R6 Only domain and application decide state transitions, retries and
  acceptance. Adapters report facts. The API and CLI observe and steer through
  use cases and contain no scheduling or business rules.
- R7 `cmd/agentplane` is the only composition root: it builds concrete adapters
  and wires them into use cases. `main` stays thin.
- R8 No global mutable state, no side-effecting `init()`, no import cycles.
  Dependencies are passed explicitly.
- R9 Export the minimum. Anything not used outside its package is unexported.

The current package layout predates these rules (for example `agent` and `gate`
import `runtime`). Until the layered refactor lands, new code must not add
violations of R1-R9, and any move toward the target layout needs a plan and an
ADR (see `prompts/refactor-clean-architecture.md`). Enforcement
(architecture test, `golangci-lint` config, metrics script) is introduced by
that refactor; until then reviewers check these rules by hand.

## Git and commit discipline

One feature should normally map to one branch/worktree.

Suggested branch names:

```text
codex/F0.1-graph-schema
codex/F0.2-graph-validator
codex/F0.4-dag-scheduler
```

Prefer small commits that describe one coherent change.

After implementation and required checks, push the feature branch and create
a Pull Request by default. Do not ask the user to choose between merging,
creating a PR, and keeping the branch. Never merge without an explicit request.

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
