# Graph package guidance

The root `AGENTS.md` applies here. `edit/AGENTS.md` adds rules for editor
operations in the child package.

## Find the owner

| Concern | Start here |
| --- | --- |
| Definition and port types | `definition.go` |
| One-document YAML parse and normalized marshal | `yaml.go` |
| Semantic diagnostics, references, cycles | `validation.go`, `loop.go`, `human.go` |
| Human-node and loop field descriptors (F2.2) | `fields.go` |
| Compiled data dependencies and value resolution | `bindings.go`, `value.go` |
| Nodes with no data-dependency path to or from a node (inspector, F2.2) | `independence.go` |
| Execution-relevant graph fingerprint | `version.go` |
| Fidelity-preserving editor operations | `edit/AGENTS.md`, `edit/doc.go` |

## Preserve these contracts

- `Parse` decodes one schema-v0 YAML document structurally. Stable schema
  fields reject unknown keys; `metadata` and node `config` are open mappings.
  Parsing does not execute config, expand environment values, read files or
  validate all graph semantics. `Marshal` normalizes YAML; do not use it to
  save an editor operation in place.
- `Validate` is deterministic and side-effect-free. Keep diagnostic codes,
  severity, definition paths and ordering stable; report independent practical
  errors together, and keep warnings nonblocking. It checks graph, loop and
  human structure, but concrete Shell, Gate and Agent configuration checks
  belong to their executors and their CLI/API composition.
- Edges are explicit data dependencies. `CompileBindings` validates first and
  derives direct producer dependencies from node-output edges only. Its
  resolution methods return typed, detached values. An absent graph input
  required by the graph or its target, a missing bound node output, or a
  mistyped value yields structured diagnostics and no usable partial map. An
  absent optional graph input may leave an optional target unset without error.
  Do not add implicit ordering edges, coercion or shared context.
- `SemanticVersion` excludes `Name` and `Metadata` and normalizes unordered
  units, edges and graph-input bindings. Presentation changes must not alter
  the execution hash. Keep production `graph` independent of runtime, API,
  desktop and concrete executors.

Before changing a durable contract, read `docs/graph-spec.md` and the active
feature spec. The base contracts are `docs/features/F0.1-graph-definition-schema-v0.md`,
`docs/features/F0.2-graph-validator-and-lint.md`,
`docs/features/F0.3-data-bindings.md`, and ADRs
`docs/adr/0001-graph-yaml-parsing-contract.md`,
`docs/adr/0002-graph-reference-validation-contract.md`,
`docs/adr/0003-data-binding-resolution-contract.md`.

## Choose regression tests

- Parsing and round trips: `yaml_test.go`.
- Diagnostics and graph rules: `validation_test.go`, `loop_test.go`,
  `human_test.go`, `unit_test.go`.
- Data flow and stable versions: `bindings_test.go`, `value_test.go`,
  `version_test.go`.

Run `go test ./internal/graph/...` before the repository gate. Update this
guide when a named owner or contract changes.
