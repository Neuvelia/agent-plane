# Security model - initial constraints

## Principle

AgentPlane executes code and commands on developer machines. Security is therefore a core runtime property, not a later UI feature.

## Phase 0 trust model

Phase 0 is a local developer prototype, but it must preserve seams needed for safer later execution.

Treat as untrusted:

- graph files;
- command arguments;
- paths and working directories;
- process output;
- future agent output;
- future remote control messages.

## Required defaults

- bind future local HTTP APIs to loopback unless explicitly configured otherwise;
- prefer structured argv execution over shell string concatenation;
- pass environment values explicitly where practical;
- do not place raw secrets in graph definitions;
- do not emit known secrets into structured events;
- preserve process cancellation/cleanup seams;
- do not send repository content to remote services implicitly;
- use least privilege for future node permissions.

## Secret rule

The graph contains only references such as:

```text
secret_ref: env/GITHUB_TOKEN
```

Secret resolution belongs to the execution environment, not the graph authoring format. The `env` provider and
Shell nodes are implemented (F3.8, ADR-0029): values are resolved once per run, delivered only to the assigned
node through its environment and redacted from events and output. This limits who receives a value and detects
leaks afterwards; it does not protect a value from the process it was granted to. The `local` (keychain) provider,
Agent grants and policy enforcement are not implemented.

A graph or `--input` that contains the resolved value of an assigned secret fails preflight with
`secret_value_in_graph` or `secret_value_in_input` before the store copies the graph and before `run_started`
(F3.8 requirement 12). The diagnostic names the secret and the location, never the value. A secret declared but
assigned to no node is not resolved, so this check cannot see it. A very short secret (1-3 characters) is redacted in
unrelated text and refuses unrelated graphs; this fail-loud behavior is a deliberate v0 trade-off.

Validation diagnostics and executor configuration errors run before that guard, so they never echo author text that
fails the grammar it was typed for: a key, id, reference or type outside the graph identifier grammar, or an
environment variable name outside the identifier grammar, is replaced by `<invalid identifier>`, a position or a
field name, and the location points at the enclosing collection (`nodes`, `inputs`). Keys of undeclared run inputs
and `--input`/`--redact-env`/`--approve`/`--reject` text that is not a declared name are never echoed, even when it looks like an identifier. Text that
matches the grammar is graph structure and is still quoted, so a secret that happens to be a valid identifier can
appear in a diagnostic; `run` and `serve` then refuse the graph with `secret_value_in_graph`, `graph validate` does
not resolve secrets and cannot. Not yet covered: YAML syntax errors from the parser library, Agent and Gate
configuration errors (`environment_refs`, `allowed_paths`, Gate inputs), and graph edit operation errors served to the
desktop editor (`internal/adapter/yamlgraph/edit`), which quote the ids and names in the requested operation.

## Future blast radius model

Expected classes:

```text
L1 contained/reversible  -> deterministic checks may auto-open the gate
L2 wide/reversible       -> stronger checks and trajectory review
L3 hard to reverse       -> human gate remains closed by default
```

Examples of L3 include production data writes, destructive migrations, deletion, and money movement.

Model confidence is not a sufficient reason to open a high-consequence gate.
