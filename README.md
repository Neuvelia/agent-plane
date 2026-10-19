# AgentPlane

AgentPlane is a local-first execution and control plane for AI coding agents.

The project focuses on orchestration, deterministic validation, bounded correction loops, execution traces, and human control. It is intentionally not another agent framework.

## Status

**Phase 0 - Execution Core is implemented.** The active planning milestone is
**Phase 1 - Agent Loop**. F1.1 provides the adapter contract and F1.2 adds
explicit installed-Codex execution with Git-derived evidence. F1.3 streaming is
the next planning candidate; gates and correction loops remain unimplemented.

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
renders the completed F0.6 event stream; `--watch` renders the same sequenced
events as they occur. Labels are presentation and do not preserve the exact
original byte stream. `--watch` displays partial log chunks immediately and
cannot be combined with `--json`.

`--json` writes one JSON document to stdout, without human progress text. The
versioned envelope has `cli_schema_version: 1`, `command`, `status`,
`diagnostics`, `failure`, and `error`. A started run also has `graph_id`,
`graph_version`, `run_id`, `nodes`, `outputs`, and `events`. Graph output values
carry `name`, `type`, and a JSON value of that type. Events use F0.6's event
schema, including base64-encoded log bytes and authoritative per-run sequence
numbers. A preflight failure has no run ID or events. A started run includes
`nodes` and `outputs` arrays even when empty. The CLI does not automatically
discover secrets in child output. Repeat `--redact-env NAME` to redact the
current value of each named environment variable from F0.6 event logs and
human log display, without putting the value in CLI arguments. The variable
must be set. Redaction does not alter graph output values explicitly exposed
by the graph.

If a human-output write fails, the CLI reports the error on stderr when
possible and exits 1. A closed stdout or stderr pipe also returns exit 1;
`--watch` cancels an active run when rendering fails. With unblocked output,
`--watch` presents the complete event sequence, including cancellation,
through replay of any retained tail. A slow or blocked output pipe may miss
late presentation when the CLI exits after cancellation.

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
event slice remains complete.

## Current milestone

`TASKS.md` records completed Phase 0 work and the Phase 1 queue. The detailed
feature catalog is in `docs/reference/full-roadmap-features.md`; an active
feature needs its own measurable spec under `docs/features/` before coding.

Keep Phase 1 work within its active feature spec. The visual IDE, cloud control
plane, billing, and managed execution remain later roadmap phases.

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
`allowed_paths` is advisory. A schema-2 `node_evidence`
event records adapter/version and paths even without a `changed_files` output
port. Event decoding retains schema-1 compatibility; CLI envelope stays v1.

Darwin/Linux process groups cover ordinary descendants with bounded shutdown;
deliberately escaped groups are outside this guarantee. Other platforms fail
closed. Progress is bounded static status text; summaries/artifacts are graph
outputs only, with usage and raw provider metadata absent. See
[ADR-0010](docs/adr/0010-codex-process-and-observed-evidence.md) and the
[real smoke evidence](docs/validation/F1.2-codex-smoke.md).
