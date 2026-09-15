# Graph Definition Schema v0 - draft

This document is the design input for feature F0.1. It is not yet an implemented contract.

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

Proposed YAML:

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

The exact binding syntax may change during F0.1/F0.3. If changed, record the decision in the feature spec or an ADR if it becomes a durable public contract.

## Identifiers

### Graph ID

- required;
- unique in its repository namespace;
- stable across content edits;
- recommended character set: lowercase ASCII letters, digits, dash.

### Node ID

- key under `nodes`;
- unique within the graph;
- stable enough to appear in events and diagnostics;
- must not depend on visual position.

## Types

Schema v0 should keep the value type system deliberately small.

Initial candidates:

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

## Graph input binding

Top-level graph inputs should be mapped explicitly to node inputs rather than becoming hidden globals.

The exact syntax is still open for F0.1/F0.3, but the semantic requirement is fixed: node readiness must be derivable from declared inputs and dependencies.

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

## Secrets

Raw secrets are forbidden in graph files.

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

Later examples:

- suspicious dependency with no consumed output;
- unreachable node;
- declared output never consumed;
- expensive serialized path that could be parallel.

Warnings should not block execution unless promoted by policy.

## Round-trip requirement

Parsing and serializing a valid graph may normalize formatting but must preserve semantic meaning.

Tests should compare normalized semantic structures, not byte-for-byte YAML formatting.
