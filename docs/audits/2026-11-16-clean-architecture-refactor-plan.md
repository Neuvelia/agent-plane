# Clean Architecture refactor - audit and plan (Steps 1-2)

Status: PROPOSED, awaiting approval. No production code has been changed.
Prompt: `prompts/refactor-clean-architecture.md`. Rules: `AGENTS.md`
("Clean code rules", "Clean architecture rules").

## 1. Baseline (measured)

- `make check` green; `go test -race ./...` clean.
- Coverage: cmd 82.1, agent 83.9, codex 86.0, api 82.3, events 84.5, gate 84.0,
  gitstate 66.7, graph 92.4, edit 89.7, runtime 90.6, store 87.3.
- `golangci-lint` 2.5.0 is installed locally (not in go.mod). Measured with
  the AGENTS.md thresholds on production + test code:
  gocyclo>10: 22 (listed below), gocognit>15: 63, nestif: 24,
  funlen>50: 46 production functions (funlen>30: 83), revive: 146 (mostly missing doc
  comments / package comments / unused receivers), errorlint: 13,
  forbidigo (`time.Now`): 4.
- Files over limits: >500 lines: `runtime/scheduler.go` 677, `graph/validation.go`
  564, `api/docs.go` 550, `api/server.go` 548. 300-500: shell/executor 491,
  events/event 482, codex/adapter 405, cli 400, events/gate 394, gitstate 325.
- Worst complexity: `events.validateProgress` 23, `events.(EvidenceReference).Validate`
  15, `gate.predicate` 15, `gitstate.ConfinedPath` 15, `shell.ExecuteNodeAttempt` 14,
  `edit.Apply` 14, `cmd.runCommand` 17 (full list reproducible with the lint config
  in stage 1).

## 2. Dependency map and rule violations

Current production imports (matches `docs/codebase-map.md`):

```text
graph, events, gitstate, version -> (none)      graph/edit -> graph
runtime -> events, gitstate, graph              shell -> graph
agent   -> events, graph, runtime               gate  -> events, gitstate, graph, runtime
codex   -> agent, events, gitstate              store -> events
api     -> events, graph, graph/edit, runtime, store
cmd     -> everything (composition)
```

| Rule | Violation | Where |
| --- | --- | --- |
| R1 domain stdlib-only | Domain model carries YAML tags and parses YAML (`go.yaml.in/yaml/v3`) | `graph/definition.go`, `graph/yaml.go`, `graph/edit/*` |
| R1/R2 inward only | Adapters import the orchestrator package: `agent`, `gate` import `runtime` for `Decision`, `ExecutionObservation`, `Executor`, `CorrectionFeedback`, `Options` | `agent/*`, `gate/*`, `cmd` |
| R1 | `api` imports `runtime` and `store` concretely (no use-case seam) | `api/server.go`, `docs.go`, `hub.go` |
| R4 clock | `time.Now` in events emitter, gate, shell | `events/emitter.go:223`, `gate/executor.go:46`, `shell/executor.go:129,131` |
| R4 process | `os/exec` + git invocation inside the orchestrator, duplicating `gitstate` | `runtime/workspace.go:45` |
| R4 randomness | `crypto/rand` for run IDs inside the domain-ish `events` | `events/event.go:32` (API token/docs ids in `api` are acceptable adapter-side) |
| R5 DTOs | API payload types mixed with handlers in large files | `api/server.go`, `api/docs.go` |
| R6 | Event emission, validation and decoding live in one package with an emitter + collector (application behavior) | `events/` |
| R8 global state | `var openPersister` function hook used as test seam | `cmd/agentplane/cli.go:315` |
| C8 `any` | `map[string]any` / `interface{}` in 7 production files: `graph/definition.go` (node `Config`), `gate/executor.go`, `shell/executor.go`, `edit/layout.go`, `api/*`, `cmd/serve.go` | see grep |
| C2/C4 | 25 functions above cyclomatic 10; 10 above 30 lines; `scheduler.go`, `validation.go`, `docs.go`, `server.go` above file hard limit | lint output |
| C12/C15 | 146 revive findings (docs, unused receivers) | lint output |

Already good (keep): consumer-owned executor interfaces in `runtime`;
`runtime` does not import concrete executors; goroutine/race hygiene clean;
test coverage high; per-package `AGENTS.md` documents contracts.

## 3. Proposed target layout

Pragmatic mapping: the existing seven concepts keep their code, we change
ownership and direction of dependencies. Fewer, larger packages than the
generic template, to respect C7 only where it pays.

```text
internal/domain/graph      <- graph (model, validation, bindings, values, versioning)  stdlib only
internal/domain/event      <- events (payload types, Validate, schema compat, decoding)  stdlib only
internal/domain/run        <- runtime state/value types: NodeState, Failure, Decision,
                              ApprovalRequest, CorrectionFeedback, ExecutionObservation, state transitions
internal/app/engine        <- runtime scheduler, loop, batch, accept, human, preflight
                              + ports it consumes: Executor, WorkspaceInspector, Clock, IDGenerator,
                              EventSink, DecisionProvider
internal/app/trace         <- events emitter + collector (sequencing, redaction, bounded progress)
internal/app/runhistory    <- list/show use cases over a RunStore port
internal/app/graphdoc      <- graph edit use cases over a GraphDocumentStore port
internal/adapter/yamlgraph <- graph YAML decode/encode DTOs <-> domain graph; graph/edit (fidelity-preserving YAML ops)
internal/adapter/shell     <- shell executor
internal/adapter/agent     <- provider-neutral agent contract executor; adapter/agent/codex
internal/adapter/gate      <- gate executor
internal/adapter/gitstate  <- git-observed workspace state (also replaces runtime/workspace.go exec)
internal/adapter/runstore  <- JSONL store
internal/delivery/httpapi  <- loopback API (handlers + DTOs split; calls use cases only)
internal/version
cmd/agentplane             <- parse args, build adapters, call use cases, exit code
```

Notes on each hard decision:

1. **YAML out of the domain.** `graph.Definition` loses `yaml:` tags; `yamlgraph`
   decodes into DTOs and maps to domain with a `SourceLocation` value so
   diagnostics keep line/column and order (ADR-0001/0002 contracts). This is the
   riskiest step; guarded by the unmodified graph tests plus a golden
   diagnostics corpus captured in stage 1.
2. **Event schema types stay JSON-tagged** in `domain/event` (documented R5
   exception: schema v9 is the contract). Emitter/collector move to `app/trace`
   and take Clock/IDGenerator ports; `domain/event` no longer calls
   `time.Now`/`rand`.
3. **`any` removal** is limited to what contracts allow: node `Config`
   (`map[string]any` from YAML) becomes a typed accessor layer
   (`NodeConfig` with validated getters); API JSON payloads use typed structs.
   Where free-form user data must remain (graph `Value`), it stays a closed
   sum type in `domain/graph`, not `any` in APIs.
4. **Decision ownership.** `agent` and `gate` stop importing the orchestrator:
   the shared value types (`Decision`, `ExecutionObservation`,
   `CorrectionFeedback`, correlation) move to `domain/run`; the executor
   interfaces stay in `app/engine` (consumer-owned) and adapters implement them.

## 4. Stages (each ends green: `make check`, `go test -race ./...`, arch test; one commit or small series each)

1. **Safety net (no production change).** Capture goldens: Phase 0 exit-graph
   CLI stdout/stderr/exit + event stream (timestamps/IDs normalized), graph
   diagnostics corpus, API response snapshots. Add architecture test
   (`go list -deps`, stdlib) in *report-only* mode listing current violations,
   `.golangci.yml` (govet, staticcheck, errcheck, errorlint, unused,
   ineffassign, gocyclo 10, gocognit 15, funlen 50, revive, gocritic, nestif,
   nolintlint, goconst, depguard, forbidigo) with a baseline so new violations
   fail, `make lint`, `scripts/metrics.sh`, CI `make lint` step (non-blocking
   until stage 6). golangci-lint stays out of go.mod.
2. **Domain extraction.** Create `domain/graph`, `domain/event`, `domain/run`;
   move pure types/rules; mechanical move first (rename imports only), then
   strip YAML/clock/rand. Arch test turns on for domain.
3. **Ports + application layer.** Introduce Clock, IDGenerator, EventSink,
   WorkspaceInspector, RunStore, GraphDocumentStore; split `scheduler.go`
   (677) by responsibility; move `trace` and `runhistory`/`graphdoc` use
   cases; delete `runtime/workspace.go` exec in favor of the git port.
4. **Adapters.** Re-home shell, agent, codex, gate, gitstate, runstore,
   yamlgraph/edit; remove adapter->orchestrator imports; adapters stop
   calling `time.Now`; replace `openPersister` hook with an injected factory.
5. **Delivery.** `httpapi` split into handlers/DTOs/hub (all files < 300),
   depends on use cases only; `cmd/agentplane` reduced to wiring + arg
   mapping (`runCommand` 17 -> <=10).
6. **Cleanup.** Burn down remaining complexity/length/`revive`/`errorlint`/
   `any` findings; flip lint and arch test to blocking; remove dead code.
7. **Docs.** ADR (layering + dependency rule + R5 exception), update
   `docs/codebase-map.md`, `docs/architecture.md`, `AGENTS.md` repository
   boundaries and the per-package `AGENTS.md` files, `prompts/review.md`.

Estimated size: large (touches ~100 production files). Stages 2-5 are
mostly moves; recommend one PR per stage, stage 1 first.

## 5. Tests

Add: architecture test; golden CLI/event/diagnostic comparisons (stage 1);
fake Clock/IDGenerator tests for `trace` and engine (removes timing reliance);
mapping round-trip tests for `yamlgraph` DTO <-> domain; contract test for
each new port with an in-memory fake. No existing assertion is weakened;
tests move with their code.

## 6. Data/state contracts affected

None by design (event v9, graph v0, API, JSONL, CLI frozen). Internal Go
import paths and package names change; `desktop/` is unaffected and
`make desktop-check` must stay green.

## 7. Risks

- YAML/diagnostic locations drift when the DTO is separated (mitigation: golden corpus, stage 2 gate).
- Event sequence/redaction behavior changes when emitter gets injected clock/IDs (mitigation: goldens with normalized time/IDs; real adapters unchanged).
- Large mechanical moves hide behavior edits (mitigation: move-only commits, then edit commits; `git diff -M` review).
- Hidden coupling via `internal/runtime` exported types used by `api` (stage 3 may need a read-model type in `domain/run`).
- Per-package `AGENTS.md` and ADR text reference old package paths.
- gitstate coverage is 66.7%; its move needs added tests first.

## 8. Ambiguities needing a decision

1. Accept moving YAML tags/parsing out of `domain/graph` into an adapter (changes `graph` public API, not contracts)? Recommended: yes.
2. Layout names `domain/app/adapter/delivery` vs keeping flat names with only dependency inversion? Recommended: the layout above.
3. One PR per stage (recommended) vs one long-lived branch.
4. Should the lint step be blocking in CI from stage 1 (with a baseline) or only from stage 6?
5. `docs/audits` vs ADR: confirm one ADR (layering) covers the whole move, or one per stage.

## 9. Decisions (approved 2026-11-16)

1. YAML moves out of `domain/graph` into an adapter: yes.
2. Layout `domain/app/adapter/delivery` as proposed: yes.
3. One PR per stage, as a stack (each PR targets the previous stage's branch);
   nothing is merged until the parallel work on `main` settles, to limit conflicts.
4. Lint is blocking in CI from stage 1, scoped to code a change touches
   (`new-from-merge-base: origin/main`), so legacy findings do not block.
5. A single ADR covers the layering decision.

## 10. Stage 2 outcome and adjustments

Delivered in stack 2/N: `graph` -> `internal/domain/graph`, `events` ->
`internal/domain/events`, `graph/edit` -> `internal/adapter/yamlgraph/edit`; YAML
parsing/encoding and the schema-v0 DTOs live in `internal/adapter/yamlgraph`;
the domain `graph` package no longer imports YAML and carries no `yaml` tags.

Adjustments to the plan, with reasons:

- **Package name `events` is kept** (`internal/domain/events`) instead of
  `event`: dozens of local variables are named `event` and would shadow the
  package. Plural is the established domain term.
- **Semantic version hashing uses an injected encoder.** `graph_version` is
  `sha256` of the canonical YAML bytes (a contract), so the domain cannot hash
  without a YAML encoder. `graph.SemanticVersion(def, graph.Encoder)` receives
  it; `runtime.Options.GraphVersion` is required and the composition root passes
  `yamlgraph.Marshal`. Stage 3 folds this into the engine's ports.
- **YAML error text is preserved.** DTO type names in decoder errors are mapped
  back to the domain names (`graph.Definition`, ...), because parse error text is
  user-visible and the editor derives line numbers from it.
- **`domain/run` extraction moves to stage 3.** Its value types (`Decision`,
  `ExecutionCorrelation`, `CorrectionFeedback`, run/node results) are only
  separable together with `ExecutionObservation`, which depends on
  `gitstate.Baseline`; that needs the `WorkspaceObserver` port introduced in
  stage 3, where `agent`, `gate` and `api` are rewired.
- **Clock/rand removal from `domain/events` moves to stage 3** with the
  Clock/IDGenerator ports and the `app/trace` split.
- Parse/validation tests that need a YAML source moved to the adapter as
  external tests (`yamlgraph_test`); domain-only tests stay in `domain/graph`.

Remaining baseline after stage 2 (`internal/archtest/baseline.txt`): 8 entries.
