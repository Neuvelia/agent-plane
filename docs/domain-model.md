# Domain model

## Core concepts

### GraphDefinition

A user-authored execution contract stored as a text file.

Owns:

- schema version;
- graph ID and metadata;
- declared graph inputs;
- node definitions;
- explicit data edges;
- declared graph outputs.

### GraphVersion

An immutable semantic snapshot of a GraphDefinition used for a run.

F0.6 identifies the schema-v0 semantic definition with a `sha256-v0:`
fingerprint that excludes name and metadata. Repository context is later work.

### NodeDefinition

Static definition of one bounded unit of work.

A node has:

- stable ID within the graph;
- type;
- declared inputs;
- declared outputs;
- type-specific configuration;
- optional node-local correction loop (F1.6);
- later: timeout, permission and policy configuration.

### EdgeDefinition

Maps a source node output to a target node input.

Invariant: an edge is a data dependency, not merely visual ordering.

### Run

One execution of one GraphVersion with resolved graph inputs.

A Run owns NodeRuns and execution events.

### NodeRun

The runtime instance of a NodeDefinition inside a Run.

A loop-enabled NodeRun has bounded Attempts (F1.6).
F0.6 gives every NodeRun an opaque ID even if it never starts.

### Attempt

One execution attempt for a node or unit.

Later attempts carry check results, artifacts, changed files, and usage metadata.

### ExecutionEvent

An append-oriented fact describing an observable execution transition.

Events are ordered within a run by sequence number, starting at 1. The
combination of run ID and sequence identifies an event.

### CheckResult

F1.5 version-1 contract returned by deterministic Gates and retained as a
schema-5 `gate_check` fact on green and red:

```text
check_result_version: 1
check_id, kind, verdict, reason
evidence[]: typed kind/source, optional typed reference
scope.checked_inputs[]: sorted unique checked port names
duration_ns: required nonnegative integer
```

A red Gate fails and exposes no accepted output; its result remains retrievable
by run ID and node-run ID with `events.CheckResults`. Operational errors do not
invent results. F1.6 adds attempts and a separate original intended correction scope. A reference is absent, known (identifier supplied), unavailable or invalid
(with diagnostic reason). File references may carry a positive line; test, diff
and artifact identifiers are opaque. References are never resolved here.
Legacy schema-4 minimal Gate payloads remain distinct and retain unknown scope
and duration. See ADR-0013.

### Artifact

A large or structured output referenced by events instead of embedded in every event.

Examples: full log, diff, report, JSON payload, patch, test output.

### Approval

Later record of a human gate decision including who, what, when, and optional comment.

### Workspace

A repository or isolated checkout where execution occurs.

The graph should reference logical workspace context, not depend on cloud storage of local absolute paths.

### Runner

An execution environment capable of running graph nodes.

Initially local. Later self-hosted and managed runners can implement the same execution contract.

## Relationships

```text
GraphDefinition 1 --- N GraphVersion
GraphVersion    1 --- N Run
Run             1 --- N NodeRun
NodeRun         1 --- N Attempt
Run             1 --- N ExecutionEvent
Attempt         1 --- N Artifact
Attempt         1 --- N CheckResult
```

## Important invariants

1. Accepted sibling output is not implicitly invalidated by another unit failing.
2. A downstream node cannot become ready until all required bound inputs are available.
3. A run must end in exactly one terminal run state.
4. A node cannot be simultaneously running and terminal.
5. Event sequence is monotonic within a run.
6. Graph files contain references to secrets, never raw secrets.
7. Visual canvas metadata must not change graph execution semantics.
8. A correction attempt never overwrites the trace of a previous attempt.
9. Unknown usage/cost is represented as unknown, never as zero.
10. Runtime state must be reconstructable from durable events once persistence is introduced.

### F1.6 attempts and accepted candidates

A loop-enabled Agent has one stable NodeRunID and up to ten scheduler-created
AttemptIDs (default three). Each attempt executes one producer and embedded
Gate cycle. A successful producer may emit attempt-correlated `node_evidence`
even when its candidate is subsequently rejected; this is producer observation,
not acceptance or a certification of repository state. Only an `accepted`
attempt publishes candidate values. Old evidence without an AttemptID retains
its success-only meaning.

`agent.AgentExecutionInput.Feedback` optionally carries typed previous AttemptID,
the exact detached sanitized CheckResult retained in trace, and the original
intended scope. The original task stays unchanged. Gate checked-input names do
not redefine correction scope. Codex places explicitly untrusted feedback on
stdin with the original process/environment/permission policy. The capability
reports transport support, not model obedience.

### F1.7 units and Return Envelopes

Optional graph `units` enrolls an unordered set of loop Agent node IDs. One
static sibling node is one retryable unit; `unit_id = node_id`. An absent
membership list preserves F1.6 behavior. No dynamic batch fanout is implied.

A version-1 Return Envelope derives verdict/reason/evidence from the exact
retained sanitized CheckResult, with source RunID/NodeID/NodeRunID/GraphVersion,
AttemptID and CheckID. Its separate complete intended scope is normalized
original allowed_paths, with an explicit scope_redacted flag. It never
substitutes checked-input names or model-reported changes for authored intent.

The coordinator registers each unit and attempt, rejects all second publication
and source/check/scope mismatches, and accepts green once. Accepted runtime
ports carry RunID, NodeRunID, accepted AttemptID and port provenance. Reads are
detached. NodeResult.AcceptedAttemptID and the matching envelope expose the
same correlation in CLI JSON without copying port values into more events.
Red candidates do not enter bindings; retries/exhaustion cannot invalidate
accepted siblings. Schema7 retains checks, envelopes and attempt completion in
that order, plus bounded declared conflict facts independent of progress.
This immutability does not extend to shared files. See ADR-0015.

### F1.8 changed-files scope check

The unit's initial Git snapshot is an ephemeral runtime baseline, not an
accepted graph value. It survives retries for that unit. The resulting
CheckResult identifies mode and authored allowed paths in typed details and
out-of-scope Git paths in sorted evidence; `scope.checked_inputs` still names
the candidate input port. A warning remains green but carries the violation
reason. A deny violation is red and enters existing correction handling. Git
inspection failure is explicitly indeterminate in warn mode and operational in
deny mode. Adapter-reported files cannot determine the verdict. Detection does
not prevent modifications or isolate the repository. See ADR-0016.
