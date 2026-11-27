# ADR-0044 - Event schema 15: run provenance, test_command checks and abandoned executors

**Status:** proposed
**Date:** 2026-11-27
**Features:** F2.9, F3.6 (#186), F1.4b (#183), F3.4 (#190)
**Amends:** ADR-0006 (event model) additively; follows ADR-0028 and ADR-0042

## Context

Three changes add facts to the event log:

1. `run_started` gains optional provenance: `repo_name`, `repo_id`,
   `repo_branch`, `repo_commit` and `runtime_version` (F2.9 run list, F3.6 run
   metadata, #186).
2. `gate_check` gains the Check Result kind `test_command` (F1.4b, ADR-0042).
3. An executor that ignores a timeout cancellation is abandoned after a bound
   (ADR-0028). Today the node just ends `cancelled` (run timeout) or
   `failed`/`shutdown` (node timeout). No fact says that the executor and the
   processes it owns may still be running after `run_finished`. Under a run
   timeout this reads as a clean cancel.

The decoder rejects unknown fields (`DisallowUnknownFields`) and validates
check kinds. A schema-14 reader that meets a log carrying these facts under the
number 14 fails with `unknown field "repo_name"` or `invalid gate kind`. The
canonical spec (section 3) says readers must instead reject a newer schema with
"unsupported event schema version". So the facts need a new schema number.

## Decision

Event schema **15** (`events.SchemaRunContext`) introduces:

- **`run_started` provenance.** The fields are optional and bounded (256
  bytes, valid UTF-8, no NUL or newline):
  - `repo_name` is the base name of the Git work tree, never a path.
  - `repo_id` is the first 16 hex digits of the SHA-256 of the work tree's
    absolute path. It tells apart two checkouts with the same base name
    without storing the path in clear text. It is a pseudonym, not
    anonymisation: an unsalted hash of a guessable path such as
    `/home/<user>/<repo>` can be reversed by trying candidates.
  - `repo_branch` is empty on a detached HEAD.
  - `repo_commit` is the full commit.
  - `runtime_version` is the engine build.

  The fields are collected from the graph's directory, not the process
  working directory. Collection is best effort and bounded at 2 seconds, so a
  provider that ignores its context cannot delay `run_started` any longer.
- **`gate_check` kind `test_command`** with the evidence rules of ADR-0042: one
  evidence item, any non-negative exit code, `green` if and only if the exit
  code is 0.
- **`executor_abandoned`**, a node-scoped fact with payload
  `{reason, bound_ms}`. `reason` is the ignored cause, `node_timeout` or
  `run_timeout`. It is emitted after `cancel_requested` and before the node's
  terminal event. It does not change the node's state rule: a node timeout
  still ends `failed`/`shutdown`, and a run timeout still ends `cancelled` with
  `run_timeout`. The fact says that the executor's goroutine, and any process
  it did not stop, may outlive the run. It carries no paths or command lines.

Writers stamp 15. Each of the three facts is rejected below schema 15 when
writing and when reading. Logs of schemas 1-14 stay readable unchanged. A
`run_started` without provenance stays valid at every schema.

Two related runtime rules ship with this schema; neither adds a fact:

- A loop unit (F1.7) whose attempt times out gets another attempt while budget
  remains, as a non-unit loop node does. The timed-out attempt has no Check
  Result, so it publishes no Return Envelope.
- The human CLI output shows the reason of a terminal fact, for example
  `Run cancelled (run_timeout)`. The `--json` document already carries it.

## Alternatives considered

### Keep schema 14 and make the fields optional

Pros: no bump.
Cons: older readers fail on unknown fields with an error that does not name
the schema, which violates canonical-spec section 3.

### Report abandonment as `process_terminated` with `leaked > 0`

Pros: no new event type.
Cons: the scheduler does not know how many processes the abandoned executor
owns. A made-up count would be a false fact.

### Record the repository path instead of a hash

Pros: readable.
Cons: puts a local path into every run log and export. The base name plus a
hash is enough to group runs.

## Consequences

### Positive

- An older reader rejects a new log cleanly. The desktop shows "unsupported
  schema" instead of a decode error.
- The run list can group by repository without merging two repositories that
  share a name.
- An abandoned executor is visible in the trace.

### Negative / trade-offs

- `repo_id` changes when a checkout moves on disk.
- `repo_id` can be reversed by guessing likely paths. The log stays local
  (ADR-0027), so this matters only when a run log is shared; a per-install salt
  would prevent it but would also change the id across reinstalls.
- `executor_abandoned` admits a leak that the runtime cannot stop: Go cannot
  kill a goroutine. Process-tree ownership (ADR-0028) is what normally prevents
  it.

## Compatibility and migration

Additive. Graph schema 0 and semantic graph versions are unchanged. Stores keep
older logs. `store.json` and `summary.json` gain `repo_id`, which they rebuild
from the log. The canonical spec section 3 table lists schema 15.

## Validation

- `internal/domain/events/runcontext_test.go`: each fact round-trips at schema
  15 and is rejected at 14; `repo_name` is a base name.
- `internal/store/testdata/schemas/v15.jsonl` round-trips.
- `internal/runtime/timeout_test.go`: `executor_abandoned` precedes the
  terminal event under node and run timeouts.
- `internal/runtime/timeout_loop_test.go`: a unit retries a timed-out attempt
  without an envelope.
- `cmd/agentplane/exitcode_test.go`: the built CLI exits 124 or 130 and shows
  the reason in both output modes.
