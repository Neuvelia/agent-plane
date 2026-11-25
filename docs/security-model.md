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

## Future blast radius model

Expected classes:

```text
L1 contained/reversible  -> deterministic checks may auto-open the gate
L2 wide/reversible       -> stronger checks and trajectory review
L3 hard to reverse       -> human gate remains closed by default
```

Examples of L3 include production data writes, destructive migrations, deletion, and money movement.

Model confidence is not a sufficient reason to open a high-consequence gate.
