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
`to: NODE_ID.inputs.INPUT_NAME`. F0.3 will resolve these references so node
readiness can be derived from declared inputs and dependencies.

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

Visual layout must be excluded from any semantic graph hash introduced later.
Metadata is therefore an open mapping and is kept separate from execution
fields in the typed definition.

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
