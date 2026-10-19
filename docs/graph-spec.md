# Graph Definition Schema v0

This document defines the YAML authoring contract implemented by feature F0.1.
Parsing produces typed Go structures but does not validate graph semantics.

## Goals

The graph format must be:

- deterministic to parse;
- human-readable and Git-friendly;
- versioned;
- explicit about inputs and outputs;
- explicit about data dependencies;
- independent from the future visual editor;
- free of embedded secrets;
- evolvable through schema versions.

## Non-goals for v0

- dynamic graph mutation;
- arbitrary expression language;
- visual ETL transformations;
- subgraphs;
- loops across nodes;
- cloud scheduling;
- organizational policies;
- implicit shared context.

## Top-level shape

YAML shape:

The following illustrates schema and binding syntax. It is not an executable
F0.5 data-passing example: strings such as `{{ inputs.message }}` are literal
Shell Node arguments, and bound inputs are not interpolated into commands.

```yaml
schema_version: 0
id: hello-parallel
name: Hello Parallel

metadata:
  description: Demonstrates two independent branches.

inputs:
  message:
    type: string
    required: true

nodes:
  left:
    type: shell
    inputs:
      message:
        type: string
    config:
      command: printf
      args: ["%s\\n", "{{ inputs.message }}"]
    outputs:
      stdout:
        type: string

  right:
    type: shell
    inputs:
      message:
        type: string
    config:
      command: printf
      args: ["%s\\n", "{{ inputs.message }}"]
    outputs:
      stdout:
        type: string

  merge:
    type: shell
    inputs:
      left_text:
        type: string
      right_text:
        type: string
    config:
      command: printf
      args: ["merge placeholder\\n"]
    outputs:
      stdout:
        type: string

edges:
  - from: left.outputs.stdout
    to: merge.inputs.left_text
  - from: right.outputs.stdout
    to: merge.inputs.right_text

graph_input_bindings:
  - from: inputs.message
    to: left.inputs.message
  - from: inputs.message
    to: right.inputs.message

outputs:
  result:
    from: merge.outputs.stdout
```

Schema v0 uses the binding reference syntax shown above. F0.1 preserves these
references as typed strings. F0.2 validates their grammar and referential
integrity after structural parsing; F0.3 resolves their runtime values.

The top-level `graph_input_bindings` field is optional so graphs without graph
inputs do not need to declare an empty list.

## Identifiers

### Graph ID

- required;
- unique in its repository namespace;
- stable across content edits;
- must follow the identifier grammar below.

### Node ID

- key under `nodes`;
- unique within the graph;
- stable enough to appear in events and diagnostics;
- must not depend on visual position.

### Identifier grammar

Graph IDs, node IDs, graph input names, node input names, node output names,
and graph output names must begin with a lowercase ASCII letter. Remaining
characters may be lowercase ASCII letters, digits, hyphens, or underscores.
Dots are not permitted because they delimit references.

### Reference grammar

References use identifiers defined above and have exactly one of these forms:

```text
NODE.outputs.PORT
NODE.inputs.PORT
inputs.INPUT
```

The first form is used by edge sources and graph outputs, the second by edge
targets and graph-input-binding targets, and the third by graph-input-binding
sources.

## Types

Schema v0 keeps the value type system deliberately small.

Declared types:

```text
string
number
boolean
json
artifact_ref
```

Avoid designing a full programming language in v0.

## Node definition

Conceptual structure:

```yaml
nodes:
  NODE_ID:
    type: shell
    inputs:
      input_name:
        type: string
        required: true
    config: {}
    outputs:
      output_name:
        type: string
```

The stable graph layer should distinguish declared contracts from type-specific configuration.

`config` is an open mapping. The graph package preserves its nested YAML data
without interpreting strings, expanding environment variables, executing
commands, resolving templates, reading files, or accessing the network. A
node-type implementation may decode the mapping into a stricter type in a
later feature.

### Shell node configuration (F0.5)

For `type: shell`, F0.5 accepts this `config` shape:

```yaml
config:
  command: go
  args: ["test", "./..."]
  cwd: /path/to/workspace
  env_allowlist: ["HOME", "PATH"]
  env:
    CI: "true"
  input_env:
    source_text: SOURCE_TEXT
  stdout_json: false
```

`command` is a required nonempty executable name or path. `args` is an
optional list of literal strings, defaulting to empty. `cwd` is an optional
working directory; omission uses the runner's current directory. Bare
executable names are resolved using the runner's PATH before launch. The
command and arguments are never passed through a system shell and are not
interpolated. Bound node inputs do not alter command, args, or cwd.

A relative `cwd` is resolved against the runner process's current working
directory at execution time, not against the graph definition file. Authors
should use an absolute path when that execution context is not fixed.

The child environment starts empty (Go adds the platform-required
`SYSTEMROOT` value on Windows). `env_allowlist` copies only named,
currently present variables from the runner; `env` adds explicit string
values and wins on duplicate names. Both fields default to empty. Unknown
configuration fields, malformed types, and invalid environment names fail
execution before the process starts. Raw secrets must not be embedded in
graph configuration.

`input_env` is an optional explicit mapping from declared string node input
names to child environment variable names. A present resolved input is copied
as its literal string value; an absent optional input creates no variable.
A missing required mapped input, a value with the wrong runtime type, or a
string value containing NUL fails the node before process launch. The mapping
rejects undeclared or non-string ports, invalid or duplicate environment
names, and names that conflict with
`env` or `env_allowlist`. Name collisions are checked without regard to case
so the same graph behaves predictably on Windows. The mapping does not read
ambient graph inputs or evaluate expressions. Event log redaction remains an
explicit runtime option; mapping an input does not automatically mark its
value as a secret.

Shell nodes may declare `stdout` and `stderr` string outputs and an
`exit_code` number output. Only declared outputs are returned. With
`stdout_json: true`, they may also declare a `json` output of type `json`;
the entire stdout must then be one valid JSON value. No JSON is extracted
from surrounding text. Invalid JSON fails the node. Output port names outside
this set fail before process launch. The ordinary F0.4 output contract checks
declared types. A nonzero exit fails the node; captured process evidence is
available on the typed execution error, not as accepted graph outputs.
After the direct child exits, the executor allows one second for inherited
stdout/stderr pipes to drain. If another process keeps them open longer after
an otherwise successful direct-child exit, the node fails with an output error
and preserves any output captured before the deadline. A nonzero direct-child
exit remains an exit failure. This does not terminate that other process.

### Agent node configuration (F1.1 contract)

`type: agent` uses the existing schema-v0 `config` mapping. F1.1 implements
this convention through the provider-neutral Agent executor. For example:

```yaml
nodes:
  implement:
    type: agent
    inputs:
      task: {type: string, required: true}
    config:
      adapter: codex
      workspace: /absolute/path/to/repository
      allowed_paths: ["src/"]
      context_refs: ["docs/architecture.md"]
      environment_refs: ["CODEX_HOME"]
    outputs:
      summary: {type: string}
      changed_files: {type: json}
```

The `adapter` ID and absolute local `workspace` path are required. Exactly one
task source is required: a nonempty literal `config.task` or a declared string
`task` input resolved by a graph binding. Both sources or neither source fail
before adapter invocation. `allowed_paths`, `context_refs`, and
`environment_refs` are optional lists of nonempty strings. They identify
intent and references only; F1.1 does not expand them, read referenced files,
resolve secrets, or copy named environment variables into a child process.
Unknown Agent configuration fields and wrong types fail at the Agent executor
boundary. The generic graph validator does not yet interpret Agent config.

An Agent node may declare only these output ports: `summary` as `string`, and
`changed_files`, `artifacts`, `raw_metadata`, and `usage` as `json`.
`changed_files` is a JSON array of `{path, provenance}` records, with a
nonempty path and provenance `reported` or `observed`. `artifacts` is a JSON
array of `{ref, name?}` records with a nonempty opaque reference. Nil slices
omit their ports; explicit empty slices produce `[]`. `raw_metadata` accepts
any valid opaque provider JSON, including `null`; the core does not interpret
its contents. `usage` contains only known `input_tokens`, `output_tokens`,
`model_name`, `provider_cost`, and `currency` fields. Token counts are integers;
cost and currency are provider-reported strings. Reported zero remains zero;
absent usage omits the port, while a present empty usage record produces `{}`.
Unknown optional data yields no output value; authors must not mark a port
`required: true` unless their adapter guarantees it. The runtime rejects
missing required values and wrong types as it does for Shell nodes. Values
become graph data only through declared ports. Provider metadata and task
text are not included in F1.1 lifecycle event payloads.

`allowed_paths` is advisory in F1.1, not a sandbox or a changed-files check.
F1.4 adds a check over explicit reported paths; F1.8 adds Git-derived scope
enforcement. See ADR-0009 for the adapter and identity decisions.

### Installed Codex execution (F1.2)

CLI selection requires `--adapter codex --codex-bin /absolute/path/to/codex`.
For this real adapter, `environment_refs` resolves only the named present
variables; invalid names or missing values fail before launch. Authentication
is owned by Codex using explicit references such as HOME or CODEX_HOME.
No ambient environment is forwarded. Context references and allowed paths
enter stdin as advisory data; neither is automatically read or enforced.

Workspace must canonicalize to a non-bare initialized Git worktree root with
HEAD. Unresolved stages, submodules and unsupported filesystem entries fail.
Git evidence compares index blob/mode/stage and working-file existence/mode/
content, including nonignored untracked files. Symlinks are hashed by link
text; symlink ancestors and unsafe paths fail. Unchanged baseline dirt is
omitted, further edits and index changes are included, and reverted edits
are omitted. Baseline paths remain in the after comparison even if newly
ignored. Initially ignored contents are not hashed; a bounded ignored-name/
directory-prefix inventory identifies newly exposed paths with unknown initial
state. Those transitions fail `codex_git` with no accepted outputs/evidence,
including new descendants beneath initially ignored directories. Establish
ignore rules before invocation and rerun. Keep other writers out of the
workspace during execution.

The final bounded JSON supplies summary and artifact references. Changed files
always come from Git with `observed` provenance. Known-empty changes are `[]`;
usage/raw metadata remain absent. Schema-2 `node_evidence` records adapter ID,
version and changes before successful `node_finished`, independently of graph
output declarations. Existing event shapes decode under schema 1 or 2;
`node_evidence` requires schema >=2. New F1.3 progress requires schema 3;
older schema 1/2 facts remain readable (ADR-0011). Failed invocations accept no outputs or
success evidence. See ADR-0010 for process ownership and protocol limits.

## Edge definition

An edge maps one declared source output to one declared target input.

```yaml
- from: producer.outputs.report
  to: consumer.inputs.report
```

Validation must reject:

- unknown source node;
- unknown target node;
- unknown output;
- unknown input;
- incompatible types;
- duplicate conflicting bindings;
- cycles in the v0 DAG.

These checks belong to F0.2. F0.1 accepts structurally valid documents even
when their references or graph topology are semantically invalid.

## Graph input binding

Top-level graph inputs should be mapped explicitly to node inputs rather than becoming hidden globals.

Schema v0 represents each mapping with `from: inputs.INPUT_NAME` and
`to: NODE_ID.inputs.INPUT_NAME`. F0.3 resolves these references so node
readiness can be derived from declared inputs and dependencies.

## Binding resolution (F0.3)

The YAML binding shape is unchanged. Each edge transfers exactly one value
from a declared node output to a declared node input. Each graph input binding
transfers one supplied graph input value to a declared node input. Values do
not come from node configuration, environment variables, hidden shared state,
or other undeclared sources. Bindings do not evaluate expressions or transform
values.

`graph.CompileBindings` first runs semantic validation. A definition with
errors yields no binding plan; warnings remain available and do not block the
plan. A plan snapshots the port contracts and parsed references so later
changes to a caller's `Definition` cannot change its behavior. Its direct
node dependencies are the distinct, sorted source node IDs of incoming
edges. Graph input bindings add no node-to-node dependency. An edge to an
optional input still declares a data dependency. There is no ordering-only
edge in schema v0.

At run entry, supplied graph input values should be checked with
`BindingPlan.ValidateGraphInputs`: required inputs must be present, supplied
names must be declared, and each supplied value must match its declared type.
For a particular node, `BindingPlan.ResolveNodeInputs` maps supplied graph
inputs and known node outputs into its declared inputs. The caller must wait
for all direct producer nodes to succeed before treating missing node outputs
as terminal errors; determining when that happens belongs to F0.4. A missing
bound node output is always an error, including when the target input is
optional. An absent optional graph input may leave an optional target input
unset, but a required graph input or required target cannot be left unsatisfied.
A present JSON `null` is a value, not an absent input. Resolution errors produce
structured diagnostics and no usable partial input map.

Values have the five declared types. Strings and booleans remain scalars;
numbers use `json.Number` to preserve decimal text; `json` values contain
validated JSON bytes, including objects and arrays. JSON bytes are copied at
the API boundary. Artifact references are nonempty opaque handles passed
through without reading artifact contents or validating artifact existence.
Declared port types and supplied runtime value types must match exactly;
there is no implicit coercion, including between JSON and scalar types.

## Graph outputs

A graph output references a node output.

```yaml
outputs:
  report:
    from: final.outputs.report
```

A graph output must reference an output that can be produced on every successful terminal path supported by v0.

## Metadata

Metadata may include non-semantic fields such as description, tags, and future canvas layout.

Visual layout is excluded from the F0.6 semantic graph fingerprint.
Metadata is therefore an open mapping and is kept separate from execution
fields in the typed definition.

F0.6 hashes a normalized schema-v0 definition with `name` and `metadata`
removed. Edge and graph-input-binding order is normalized; sequence-valued
node configuration such as `args` keeps its authored order. The resulting
version uses a `sha256-v0:` prefix so a future algorithm can be distinguished.

## Parsing and serialization

- A graph definition is exactly one YAML document.
- `schema_version` is required, must be a YAML integer, and schema v0 is the
  only supported version.
- Unknown fields fail at the graph, graph input, node, port, edge, graph input
  binding, and graph output levels.
- Arbitrary keys are allowed only inside the explicit `metadata` and `config`
  extension mappings.
- Malformed YAML, an unsupported version, an unknown typed field, or multiple
  YAML documents returns an error and no partial definition.
- Serialization accepts only schema v0 definitions.
- YAML formatting may be normalized during serialization.

## Secrets

Raw secrets are forbidden in graph files. Schema v0 has no generic `secret` or
`secrets` field, and strict parsing rejects such undeclared top-level fields.
Because node `config` is intentionally opaque, the parser cannot reliably
identify whether an ordinary configuration string contains sensitive data;
authors and later node implementations must not place raw secrets there.

Later graphs may reference named secrets:

```yaml
secrets:
  github_token:
    secret_ref: local/github-token
```

This is outside Phase 0 unless required by a later activated feature.

## Validation categories

### Blocking errors

Examples:

- invalid schema;
- duplicate IDs;
- missing node/type;
- missing referenced node/input/output;
- incompatible binding;
- cycle;
- required input with no source.

### Warnings

F0.2 reports a warning for a declared node output that is not consumed by a
valid, type-compatible edge or exposed as a graph output. Invalid edges do not
count as consumers. This warning does not block execution.

Unreachable-node analysis and parallelism suggestions are later work.

Warnings should not block execution unless promoted by policy.

## Round-trip requirement

Parsing and serializing a valid graph may normalize formatting but must preserve semantic meaning.

Schema v0 uses the complete decoded `Definition` as its normalized round-trip
representation. Tests compare typed structures, not byte-for-byte YAML or map
iteration order. A semantic hash is outside F0.1.

## Gate node (F1.4)

Each `type: gate` has one nonempty `check_id` (at most 256 UTF-8 bytes) and
`kind`. Unknown or irrelevant config fields are rejected in stable key order
with the node ID. Referenced ports must be declared required with the types
below. Only `verdict: {type: string}` may be declared as an output.

| Kind | Config and bound inputs |
| --- | --- |
| `command` | `command: /usr/bin/diff`, canonical absolute `cwd`, `left_input` and `right_input` name required string ports containing relative file paths |
| `json_predicate` | `input` names a required JSON object port; `field` is a nonempty list of literal object keys, `operator: eq` or `ne`, exactly one of `expected` scalar/null or `expected_json` containing a JSON scalar/null literal string |
| `reported_file_scope` | `input` names a required JSON array port of `{path, provenance?}` records, `allowed_paths` is an explicit list of exact file paths or directory prefixes ending in `/` |

Numeric equality is mathematical without float rounding (1 equals 1.0,
numbers differ from strings). For decimal/exponent values or integers beyond
the native YAML integer range, use `expected_json: '1.00000000000000000001'`.
Floating-point `expected` shorthand is rejected because opaque YAML decoding
may already lose precision. Generic config parsing and graph hashing do not
change. Missing predicate fields give red even for `ne`; malformed/traversed
non-object or non-scalar values are errors.

Paths reject absolute/drive paths, NUL, backslashes and any parent traversal
segment. Dot/repeated separators normalize; directory prefixes match segment
boundaries. Scope evidence is always `source: reported`, sorted/deduplicated,
and bounded with an omitted count; it does not assert Git verification.

Command checks support Darwin/Linux trusted `/usr/bin/diff` only. No args,
options, alternate binary, scripts, shell or caller environment may be supplied.
Both files must be regular and confined under a symlink-free explicit cwd.
The implementation preopens them read-only and supplies only `/dev/fd/3` and
`/dev/fd/4` operands, `LC_ALL=C`, context termination and a one-second pipe-drain
limit. Exit 0 is green, 1 red; other exits/start/cancellation are errors.
This is a restricted read-only operation trusting OS binary integrity, not a
sandbox against hostile concurrent filesystem changes.

Green publishes only the declared `verdict: green` and runs dependents once;
red fails and blocks dependents with no accepted outputs. Both emit schema-5
`gate_check` before `node_finished`, with version-1 check result identity,
kind/verdict/reason, typed evidence, required nonnegative `duration_ns` and
`scope.checked_inputs` containing actual checked port names. Each stream
retains at most 8192 bytes after streaming redaction,
with omitted safe-byte counts; byte evidence serializes as base64. Facts have
at most 32 evidence items and 32768 encoded payload bytes, are validated and
copied before retention, and remain available after progress/live omissions.
Readers accept event schemas 1–5; schema-4 Gate facts decode as distinct legacy
minimal payloads without invented duration/scope. Full results require schema 5;
CLI envelope remains version 1. Optional evidence references distinguish known
identifiers, unavailable or invalid diagnostic references, and absence. File
references may include positive lines; test/diff/artifact identifiers are opaque.
No reference lookup or correction permission is inferred from these fields.
See ADR-0012, ADR-0013 and `docs/examples/gate.yaml`.
