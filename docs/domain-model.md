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
- later: timeout, permission, loop, and policy configuration.

### EdgeDefinition

Maps a source node output to a target node input.

Invariant: an edge is a data dependency, not merely visual ordering.

### Run

One execution of one GraphVersion with resolved graph inputs.

A Run owns NodeRuns and execution events.

### NodeRun

The runtime instance of a NodeDefinition inside a Run.

A future NodeRun may have multiple Attempts when correction loops are enabled.
F0.6 gives every NodeRun an opaque ID even if it never starts.

### Attempt

One execution attempt for a node or unit.

Later attempts carry check results, artifacts, changed files, and usage metadata.

### ExecutionEvent

An append-oriented fact describing an observable execution transition.

Events are ordered within a run by sequence number, starting at 1. The
combination of run ID and sequence identifies an event.

### CheckResult

Later contract returned by deterministic gates:

```text
verdict
reason
evidence[]
scope
check_id
duration
```

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
