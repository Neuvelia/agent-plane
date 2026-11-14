# AgentPlane

AgentPlane is a local-first execution and control plane for AI coding agents.

The project focuses on orchestration, deterministic validation, bounded correction loops, execution traces, and human control. It is intentionally not another agent framework.

## Status

**Phase 0 - Execution Core and Phase 1 - Agent Loop (F1.1-F1.8) are implemented;
the real-Codex F1.7 demo is prepared but not yet executed.** F1.1 provides the adapter contract and F1.2 adds
explicit installed-Codex execution with Git-derived evidence. F1.3 adds bounded
normalized progress and live delivery. F1.4 adds deterministic read-only Gates;
F1.5 adds versioned Check Results with checked-input scope, duration and typed
evidence references. F1.6 adds bounded node-local correction attempts. F1.7 adds
explicit static units, Return Envelopes and immutable accepted runtime values/
provenance (ADR-0015). F1.8 adds opt-in Git-observed changed-files scope checking
(ADR-0016). Shared files remain outside the immutability guarantee.

A Phase 2 vertical slice adds a `human` approval node, a Shell correction loop,
an opt-in local run store (`--store-dir`, `runs list/show`), a loopback `serve`
API, and an Electron desktop shell in `desktop/` (see `desktop/README.md`).

The CLI-driven Phase 0 runtime executes independent Shell nodes concurrently,
passes explicit data between nodes, emits structured events, and finishes
deterministically.

## Start here

1. Read `AGENTS.md`.
2. Read `docs/architecture.md` and `docs/graph-spec.md`.
3. Read `docs/roadmap.md` and the active feature spec under `docs/features/`.
4. Create a branch or worktree for the selected feature.
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
./bin/agentplane graph validate .agents/graphs/hello-parallel.yaml
./bin/agentplane run .agents/graphs/hello-parallel.yaml --max-concurrency 2
./bin/agentplane run .agents/graphs/hello-parallel.yaml --watch
./bin/agentplane run .agents/graphs/hello-parallel.yaml --json
./bin/agentplane run .agents/graphs/phase0-cli.yaml --input message=hello --json
```

## CLI runner

```text
agentplane graph validate PATH [--json]
agentplane run PATH [--input key=value]... [--redact-env NAME]... [--max-concurrency N] [--watch | --json]
agentplane version
```

The graph path is relative to the current process directory. Shell node
`cwd` and relative executable paths retain the same process-directory
semantics; they are not resolved relative to the graph file. The CLI does not
change the working directory. The default concurrency limit is 2.

Repeat `--input` for distinct graph input names. The first `=` separates the
name from the value, so `--input message=a=b` supplies `a=b`. Duplicate names
are usage errors. Values use the declared graph type: strings stay literal,
numbers use JSON number syntax, booleans require `true` or `false`, JSON must
be one valid JSON value, and artifact references are nonempty opaque strings.
Unknown and missing required names are graph input errors. Inputs are bound to
node ports. Shell nodes can expose selected string inputs to the child through
an explicit `input_env` mapping; values are never interpolated into argv.

The `phase0-cli.yaml` example has three Shell nodes, two independent branches,
explicit graph and edge bindings, and a deterministic final graph output. Its
Shell nodes map selected string inputs to child environment variables with
`input_env`; the dependent output includes both branch values. The example
uses `go run` for a portable helper process, so run it from the repository
root with the Go toolchain installed. Other graphs may use any available
executable and do not require Go at execution time.

Human status and diagnostics go to stderr. Node stdout goes to stdout and
node stderr goes to stderr, each labeled with node ID and stream. Default mode
renders retained events after completion; `--watch` renders a live sequence
prefix immediately and replays its retained tail after completion. Labels are presentation and do not preserve the exact
original byte stream. `--watch` displays partial log chunks immediately and
cannot be combined with `--json`.

`--json` writes one JSON document to stdout, without human progress text. The
versioned envelope has `cli_schema_version: 1`, `command`, `status`,
`diagnostics`, `failure`, and `error`. A started run also has `graph_id`,
`graph_version`, `run_id`, `nodes`, `outputs`, and `events`. Graph output values
carry `name`, `type`, and a JSON value of that type. Events use event schema 9 (old schema 1–8 records remain readable), including base64-encoded log bytes and authoritative per-run sequence
numbers. A preflight failure has no run ID or events. A started run includes
`nodes` and `outputs` arrays even when empty. The CLI does not automatically
discover secrets in child output. Repeat `--redact-env NAME` to redact the
current value of each named environment variable from event logs, normalized
progress string fields, evidence and their human display, without putting the value in CLI arguments. The variable
must be set. Redaction does not alter graph output values explicitly exposed
by the graph.

If a human-output write fails, the CLI reports the error on stderr when
possible and exits 1. A closed stdout or stderr pipe also returns exit 1;
`--watch` cancels an active run when rendering fails. With unblocked output,
`--watch` presents retained lifecycle/evidence, including cancellation, through
replay without duplicates. Progress can be omitted: retained logs/progress are
bounded to 4 MiB encoded whole-event JSON / 4096 items; the live FIFO is bounded
to 1 MiB / 1024 items including its in-flight head. Retention drops new progress
when full; first live overflow seals admission so delivered events form a
sequence prefix. Sequence gaps are expected. A final `progress_omitted` marker
reports separate retained/live omitted events and encoded bytes. The marker and
`run_finished` are replay-only after the delivery pump stops and joins. Slow
readers cannot block the scheduler. A blocked CLI output pipe can still block
presentation. Lifecycle/evidence memory remains proportional to the graph and
evidence size; legacy Shell output capture is unchanged. These are progress
bounds, not a constant-total-memory guarantee.

If JSON cannot be written, the CLI reports the write error on stderr and
exits 1; stdout may contain an incomplete document in that case.

| Exit code | Meaning |
| --- | --- |
| 0 | Valid graph or successful run |
| 1 | Internal CLI, serialization, or output write error |
| 2 | Invalid command, flag, or `--input` syntax |
| 3 | Graph file, parse, validation, or input preflight error |
| 4 | Started graph run failed |
| 130 | Run cancelled by Ctrl+C or SIGTERM |

On Ctrl+C or SIGTERM, the CLI cancels the run context and waits for the
scheduler. The current Shell executor terminates its direct child;
process-tree termination is outside Phase 0. If a watch output pipe stops
reading, cancellation may omit late live presentation, while the retained
lifecycle/evidence remains complete and progress loss is explicit.

## Current milestone

`docs/roadmap.md` is the current engineering index. The historical Phase 0 and
Phase 1 checklist is in `docs/reference/implementation-queue.md`. A root
`TASKS.md`, when present, is a local worktree journal for ongoing agent work.
The detailed feature catalog is in `docs/reference/full-roadmap-features.md`;
an active feature needs its own measurable spec under `docs/features/` before
coding.

Keep each active feature within its own spec. The cloud control plane, billing,
and managed execution remain later roadmap phases.

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

## Installed Codex adapter (F1.2)

Opt in explicitly to authenticated Codex execution:

```bash
agentplane run graph.yaml --adapter codex --codex-bin /absolute/path/to/codex --json
```

The Agent node uses `adapter: codex`, an absolute Git worktree root with HEAD,
and explicit `environment_refs` such as `[HOME, PATH]` for authentication and
tools. Only referenced variables are forwarded. The supported protocol was
verified against Codex CLI `0.163.0-alpha.10.2`; required flags are detected at
run time. Codex uses `workspace-write`, approval policy `never`, no shared
daemon, and ignored personal configuration while retaining execution rules.

Changed paths compare working-file and index snapshots before/after execution;
unchanged dirty/staged/untracked files are omitted. Evidence records observed
changes, not authorship. Keep the workspace otherwise idle. Ignored/outside
contents are excluded. Baseline paths remain observable if newly ignored;
newly exposed initially ignored files/directories fail safely because their
pre-run contents are unknown. Set ignore rules before execution and rerun.
`allowed_paths` is advisory. A schema-3 `node_evidence`
event records adapter/version and paths even without a `changed_files` output
port. Event decoding retains schema-1 compatibility; CLI envelope stays v1.

Darwin/Linux process groups cover ordinary descendants with bounded shutdown;
deliberately escaped groups are outside this guarantee. Other platforms fail
closed. Progress is bounded static status text; summaries/artifacts are graph
outputs only, with usage and raw provider metadata absent. See
[ADR-0010](docs/adr/0010-codex-process-and-observed-evidence.md) and the
[real smoke evidence](docs/validation/F1.2-codex-smoke.md).

## Normalized Agent progress (F1.3)

Schema 3 adds `text_delta`, `tool_call_started/finished`,
`command_started/finished`, `file_changed`, `status` and `progress_omitted`.
Runtime-owned run/node IDs correlate progress; attempt IDs remain empty. The
optional normalized adapter interface preserves legacy adapters and callbacks.
Codex projects supported JSONL public item fields: completed assistant text,
tool names/statuses, command IDs/statuses/exit codes, reported changed paths,
and finite statuses. Completed text does not imply token streaming. Command
argv/output, tool arguments/results, provider errors, unknown records and
private reasoning are excluded. Reported file progress is distinct from final
Git-derived observed evidence.

Observed executors require UTF-8-safe redaction values and a UTF-8-safe
secret-absent marker; unsafe configurations fail before execution. Normalized
string fields reject invalid UTF-8 before JSON encoding. Legacy byte-only log
executors retain arbitrary-byte redaction and base64 serialization.

Normalized content is capped at 16 KiB; public text is redacted before splitting
on UTF-8 boundaries. Structured records exceeding that cap are ignored.
Redaction holds only undecidable secret-prefix tails, with at most 16 active
text source IDs; completed tails release their slots. If malicious/interleaved
sources exhaust those slots, new sources are ignored while full and a finite
`progress_limited` status is emitted once. The memory for secret tails depends
on configured secret lengths. Malformed, duplicate-key and oversized optional
Codex records are skipped, draining oversized lines with bounded storage. Valid
following progress and final results still succeed; explicit provider failure,
process failure and invalid required final results still fail. See
[ADR-0011](docs/adr/0011-bounded-normalized-progress.md).

## Deterministic Gates (F1.4)

Gate nodes accept explicit bound data and emit a typed `gate_check` fact before
finishing. Green publishes the declared `verdict: green`; red fails the Gate
and skips dependents without publishing accepted outputs. Operational errors
and cancellation never manufacture a verdict. Gate and Shell work without
an installed agent or remote service.

Checks support exact JSON scalar predicates, advisory reported-file scope,
and fixed `/usr/bin/diff` comparisons of confined regular files. Arbitrary
commands, options and environment injection are rejected. The trusted system
diff reads preopened descriptors; this narrow operation is not a general
process sandbox. Evidence captures 8192 redacted bytes per stream, counts
omissions, and labels reported scope explicitly. See
[Gate contract](docs/graph-spec.md#gate-node-f14) and [ADR-0012](docs/adr/0012-gate-node-contract.md).

Try `go run ./cmd/agentplane run docs/examples/gate.yaml --input 'candidate={"ready":true}'`.
Use `false` to see red block the dependent Shell.

F1.5 results use `check_result_version: 1`, required `duration_ns` (zero is
known zero), and `scope.checked_inputs` listing the actual checked port names.
This scope does not grant correction permissions. Each evidence item has a
kind/source and may include a file (optional positive line), test, diff or
opaque artifact reference. Reference state `known` means an identifier was
supplied; `unavailable` and `invalid` carry diagnostic reasons. No resource
lookup is performed. Schema 4 minimal Gate facts decode separately without
invented scope/duration. See [ADR-0013](docs/adr/0013-check-result-contract.md).

## Scoped unit retry (F1.7)

Opt in with top-level `units: [a, b, c, d]`, an unordered set of loop Agent
node IDs (at most 256 UTF-8 bytes per enrolled ID). Each sibling node is one
static retryable unit. Existing bindings select accepted green values; red
candidates stay private and only their unit receives another bounded attempt.
The coordinator accepts each unit once and preserves accepted port provenance.
CLI nodes expose accepted_attempt_id, and retained schema7 Return Envelopes link
the exact sanitized check/attempt/source. Absent units preserves F1.6 behavior.

All Agent producer/check cycles sharing a checkout serialize in unit-mode.
Distinct proven physical Git worktrees can run concurrently; advisory disjoint
paths do not grant concurrency in a shared checkout. Bounded declared conflict
facts describe potential overlap/unknown scope or identity. Scope is advisory:
accepted runtime values remain immutable even if another unit changes their
files. Git-observed scope enforcement is F1.8 (below); filesystem isolation remains F3.1 work.

See [the example graph](docs/examples/scoped-retry.yaml),
[ADR-0015](docs/adr/0015-return-envelope-and-unit-acceptance.md) and the
[prepared after-merge real-Codex demo](docs/validation/F1.7-real-codex-demo.md).
The recipe is not a record of a completed real-provider validation.

## Changed-files scope check (F1.8)

An Agent loop may use embedded Gate `kind: changed_files_scope` with
`mode: warn|deny`. The runtime captures a Git baseline before the first attempt
and compares actual index/worktree state afterwards, including untracked,
ignored and deleted files; renames check both paths. Adapter-reported
`changed_files` are metadata only. Deny violations are red and enter the bounded
correction loop; warn violations stay green with visible evidence. This detects
out-of-scope writes after the fact; it does not prevent them or isolate the
workspace (F3.1). See the [example graph](docs/examples/changed-files-scope.yaml),
[graph-spec](docs/graph-spec.md) and [ADR-0016](docs/adr/0016-changed-files-scope-check.md).
