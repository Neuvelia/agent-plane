# Codex prompt - Clean Architecture / Clean Code refactor of `cmd/agentplane` and `internal/`

```text
You are refactoring AgentPlane (Go). Scope: `cmd/agentplane/` and `internal/`
only. `desktop/` is out of scope and must keep working unchanged.

This is a BEHAVIOR-PRESERVING, STRUCTURAL refactor. It is not a feature.

## 0. Hard constraints (frozen contracts)

Zero changes to any external contract:
- event schema (v9) and event JSON shape/field names/ordering semantics;
- graph schema and parsing/validation results (including error messages that
  tests or docs assert on);
- loopback API routes, payloads, status codes;
- JSONL run-store format;
- CLI flags, exit codes, stdout/stderr output.

Existing tests are the contract. You may change imports, package names and
test helpers/locations, and you may move a test with the code it covers. You
may NOT weaken, delete or loosen an assertion. If a contract test must change,
STOP and report it as a blocker; do not "fix" the test.

All AGENTS.md rules still apply (local-first, edges are data dependencies,
bounded retry, trace is correctness, untrusted-input handling, stdlib-first
dependency policy). If a refactor step would weaken any of them, stop and say so.

## 1. Process (do not skip)

Step 1 - Read: AGENTS.md, docs/codebase-map.md, docs/architecture.md,
docs/domain-model.md, docs/development-workflow.md, docs/testing-strategy.md,
all ADRs in docs/adr/, and every package you will touch.

Step 2 - Audit and plan (STOP after this step and wait for approval):
  a. Dependency map of current packages (who imports whom) and every violation
     of the rules in section 2.
  b. Per-package list of clean-code violations (section 3), with file:line,
     ranked by severity. Measure with tools, do not eyeball.
  c. Target package layout (section 4) with a mapping old -> new for every
     file, and the ports (interfaces) to introduce, each with its owner package.
  d. Ordered migration stages (section 5), each independently green.
  e. Risks, ambiguities, and anything that needs an ADR.
  Keep `TASKS.md` updated per AGENTS.md.

Step 3 - Execute stage by stage. After EACH stage: `make check` must be green,
the architecture test (section 6) must pass, and you commit (one coherent
change per commit, conventional-commit messages). Never carry a red stage
into the next one. No `//nolint` without a one-line justification.

Step 4 - Final report per AGENTS.md "Step 6", plus: before/after dependency
graph, before/after metrics (section 3 thresholds), list of ADR(s) written.

## 2. Clean Architecture rules (enforced)

Layers, from inside out:

1. DOMAIN (entities, value objects, domain services, domain errors):
   graph model and validation, binding resolution, run/node/attempt state
   machine, event types, check/gate result types, return envelope.
   Imports: standard library only (no other layer, no third-party).
2. APPLICATION (use cases / orchestration, defines ports):
   scheduler/run orchestration, correction loop, gate evaluation flow,
   approval flow, run-query use cases (list/show), graph-edit use cases.
   Imports: domain. Declares the ports it needs as small interfaces in the
   package that CONSUMES them (Go style): Clock, IDGenerator, EventSink,
   RunStore, ShellRunner, AgentRunner, WorkspaceObserver (Git state),
   GraphRepository, etc.
3. ADAPTERS / INFRASTRUCTURE (implements ports, talks to the world):
   shell executor (os/exec), Codex agent adapter, gitstate (git CLI),
   JSONL run store, filesystem graph loader, YAML parsing.
   Imports: domain + application ports. Never imported by domain/application.
4. INTERFACE / DELIVERY: loopback HTTP API (`internal/api`) and the CLI.
   Translate transport <-> use cases. No business rules, no scheduling,
   no retry or state decisions (the API only observes and steers).
5. COMPOSITION ROOT: `cmd/agentplane` only. The only place that constructs
   concrete adapters and wires them into use cases. `main` stays thin:
   parse args -> build dependencies -> call a use case -> map result to exit code.

Rules:
- R1 Dependency rule: source dependencies point inward only
  (delivery/adapters -> application -> domain). Domain imports nothing from
  the project. Application never imports an adapter or delivery package.
- R2 No adapter imports another adapter. Cross-adapter needs go through a port.
- R3 Ports are small (1-3 methods), consumer-owned, named for the role
  (not `XxxImpl`/`IXxx`). Return concrete types, accept interfaces.
- R4 Domain/application code never touches os, os/exec, net/http, the
  filesystem, time.Now, rand, or env directly: inject via ports
  (Clock, IDGenerator, FS/Process runner). This is what makes tests
  deterministic and sleep-free.
- R5 Serialization types (YAML/JSON DTOs, API payloads, JSONL records) live in
  adapters/delivery and are mapped to/from domain types. Domain types carry
  no struct tags for transports unless the event schema contract requires it;
  if it does, isolate that in a dedicated event-contract package and document why.
- R6 Cross-layer data crosses boundaries as plain structs/values, not as
  framework or transport types.
- R7 No global mutable state, no `init()` side effects, no package-level
  singletons. Dependencies are passed explicitly (constructor injection).
- R8 `context.Context` is the first parameter of every cancellable or I/O
  operation and is never stored in a struct.
- R9 No import cycles; no "util", "common", "helpers", "types" or "base"
  packages. Packages are named for what they provide, not what they hold.
- R10 `internal/` sub-structure is the only visibility mechanism; export the
  minimum. Anything not needed outside its package is unexported.
- R11 Only DOMAIN and APPLICATION may decide state transitions, retries and
  acceptance. Adapters report facts; they never decide.

## 3. Clean Code rules (enforced; thresholds are mechanical)

Naming
- C1 Names reveal intent; no abbreviations except universal ones (ctx, err, id).
  Packages: short, lowercase, singular, no stutter (`graph.Graph` is fine,
  `graph.GraphValidator` is not). Booleans read as predicates.
Functions
- C2 One level of abstraction per function; it does one thing. Target
  <= 30 lines of body, hard limit 50 (excluding table-driven test data).
- C3 <= 4 parameters (excluding ctx); more -> parameter struct or redesign.
  No boolean "flag" parameters that switch behavior: split the function.
- C4 Cyclomatic complexity <= 10, cognitive complexity <= 15 per function.
  Nesting depth <= 3; prefer early returns / guard clauses over else-chains.
- C5 Command-query separation: a function either changes state or returns
  information, not both (except documented, conventional cases like
  `Pop`/`Next`).
- C6 No hidden side effects; no output parameters; no named-return tricks
  that obscure flow.
Types and files
- C7 Single Responsibility: one reason to change per type/package. Target
  <= 300 lines per file, hard limit 500; <= 7 exported methods per type
  before you must justify or split it.
- C8 Prefer small, explicit types over `map[string]any` / `interface{}` /
  `any` at every stable boundary. No `any` in domain or application APIs.
- C9 Make illegal states unrepresentable: typed IDs, typed enums with
  validated constructors instead of raw strings; constructors enforce invariants.
- C10 Composition over inheritance/embedding-for-reuse; embed only for
  genuine interface promotion.
Duplication and dead code
- C11 DRY on knowledge, not on text: remove duplicated logic and duplicated
  rules; do not create abstractions for two similar lines.
- C12 No dead code, unused params/fields/exports, commented-out code, or
  stale TODOs. Remove them (verify with `staticcheck`/`unused`).
Errors
- C13 No panics for expected failures. Wrap with `%w` and context
  (what failed, which graph/node/binding, how to fix). Sentinel errors /
  typed errors for conditions callers branch on; compare with
  `errors.Is/As`, never by string. Never swallow an error; never both log
  and return it.
- C14 Domain errors are domain types; adapters translate infrastructure
  errors to domain/application errors at the boundary.
Comments and docs
- C15 Code explains itself; comments state invariants, non-obvious decisions
  and contract references (ADR/spec ids), never restate the code. Every
  exported identifier has a doc comment starting with its name.
Concurrency
- C16 Every goroutine has a clear owner, a cancellation path via ctx, and a
  bounded lifetime; no goroutine leaks (test with `goleak`-style checks or
  explicit sync). Prefer channels/sync primitives over shared mutable state;
  document lock ordering. `go test -race` must be clean.
Tests
- C17 Tests prove contracts, not implementation. Table-driven where it
  clarifies; one behavior per test; names describe behavior. No sleeps for
  synchronization (use barriers/channels/fake clock). Use fakes implementing
  ports instead of mocking frameworks. Test code obeys the same naming and
  duplication rules (shared helpers in an `xxxtest` package, as `adaptertest`).
  Coverage of changed code must not decrease.

## 4. Target layout (propose concretely in Step 2; this is the expected shape)

```text
cmd/agentplane/            composition root + CLI parsing only
internal/domain/...        graph, run, event, check (stdlib only)
internal/app/...           use cases and port definitions
internal/adapter/...       shell, agent/codex, gitstate, store (jsonl), graphfile/yaml
internal/delivery/...      api (loopback HTTP), cli presentation
internal/version/          build metadata
```

You may adjust names/granularity if you justify it in the plan, but the layer
rules in section 2 are non-negotiable. Do not split into more packages than
the dependency rule and C7 require.

Known current smells to address (verify, do not assume):
- `agent` and `gate` import `runtime` (adapter/feature code depending on the
  orchestrator) -> invert via ports owned by the application layer.
- `runtime/scheduler.go` (~680 lines) and `api/server.go`/`api/docs.go`
  (~550 each), `graph/validation.go` (~560), `events/event.go` (~480):
  split by responsibility.
- `store` depends on `events`; `api` reaches into runtime, store, graph, edit:
  route through use-case ports.
- `cmd/agentplane/cli.go` (~400 lines): reduce to wiring + arg mapping.

## 5. Staging (adapt in the plan)

1. Safety net: record baseline (`make check`, `go test -race ./...`,
   coverage, size/complexity metrics). Add the enforcement from section 6.
   No production code changes yet.
2. Domain extraction: move pure types/rules inward; eliminate non-stdlib and
   cross-layer imports from them.
3. Ports + application layer: introduce consumer-owned ports (Clock, IDs,
   EventSink, RunStore, ShellRunner, AgentRunner, WorkspaceObserver); move
   scheduler/loop/gate/approval orchestration behind them.
4. Adapters: re-home shell, codex, gitstate, store, yaml loader to implement
   the ports; remove adapter->orchestrator imports.
5. Delivery: API and CLI become thin translators; composition root wires all.
6. Cleanup: dead code, naming, comments/docs, remaining threshold breaches.
7. Docs: update docs/codebase-map.md, docs/architecture.md and add an ADR
   recording the layering decision and the dependency rule (use
   docs/adr/0000-template.md). Update AGENTS.md "Repository boundaries".

Each stage: smallest possible diff that moves the repo one step closer;
no opportunistic behavior changes; no new features.

## 6. Enforcement (add in stage 1, keep green)

- Architecture test (Go test using `go list -deps`/`go/build` or
  `go/packages`, stdlib-first) that fails when a forbidden import appears per
  R1/R2 (domain imports nothing internal; application never imports
  adapters/delivery; adapters never import each other or delivery).
- `.golangci.yml` enabling at least: govet, staticcheck, errcheck,
  errorlint, unused, ineffassign, gocyclo (10), gocognit (15), funlen
  (50 lines), revive (exported docs, naming), gocritic, nestif,
  nolintlint, goconst, depguard (encode the layer rules), forbidigo
  (`time.Now`, `os.Getenv`, `panic` outside `main`/tests).
  golangci-lint is a development-only tool: per the dependency policy,
  state this in the plan; do not add it to go.mod and keep `make check`
  runnable without it (add `make lint` as a separate target that is run
  in CI/locally when available).
- Metrics script under `scripts/` (no new runtime deps) reporting file and
  function length violations; thresholds from section 3.

## 7. Definition of done

- `make check` and `go test -race ./...` green; `make desktop-check` still
  green (the desktop talks to the same API);
- architecture test and (if available) `make lint` green, zero new `nolint`;
- no function/file above the hard limits in section 3;
- no contract change (event, graph, API, store, CLI) - verified by the
  unmodified contract tests plus a golden comparison of CLI output and an
  event stream for the Phase 0 exit-criterion graph before/after;
- docs and ADR updated; AGENTS.md boundaries section matches the new layout;
- final report with metrics before/after and known limitations.

Begin with Step 1 and Step 2 only. Do not modify code until the plan is approved.
```
