# ADR-0029 - Secret references and execution policy v0

**Status:** proposed
**Date:** 2026-11-17
**Features:** F3.8 (`docs/features/F3.8-secrets-references.md`), F3.9 (`docs/features/F3.9-execution-policy-v0.md`)

## Context

Tarmind (in code today: agentplane) runs commands and coding agents with the full authority of the user.
Redaction today is an opt-in list of exact values (`--redact-env`), unrelated to which node may use a value.
F1.8 detects out-of-scope writes after the fact; F3.1 isolates worktrees but is not a sandbox, and both defer
confinement to F3.9. Secret handling and permissions are one trust question: if secrets were resolved by a
separate path from policy, a secret could reach a node the policy does not admit. The dependency policy also
forbids new Go dependencies without strong justification, so enforcement must use the standard library, the
kernel and the OS.

## Decision

1. **One pipeline, one order, before `run_started`.** Validate graph, evaluate policy (pure), probe platform,
   decide per-permission enforcement, deliver-or-deny each secret grant, resolve only admitted secrets, build the
   redaction set, print the permissions report. Failures are preflight diagnostics, not events. Secret providers
   are reachable only from this pipeline; an architecture test enforces that only the composition root imports
   providers and platform enforcement.
2. **Graph holds references only.** Additive optional top-level `secrets` (`name -> {secret_ref}`) and
   `config.secret_env` (env name -> secret name) on Shell and Agent nodes (Agent deferred: C4.0 review #138 F-2; validation returns `E_SECRET_ENV_AGENT_UNSUPPORTED` until the Codex spike, F3.8 Open question 6); additive top-level `policy` and
   `config.policy`. No graph `schema_version` bump (as `units`). Graph defaults restrict but never grant secrets;
   a node value may only narrow a default.
3. **Secret delivery is environment-only, per node run, resolved once in process memory.** Never argv, file,
   prompt, event, store or API. First provider `env` (`env/NAME`); `local` is reserved for the OS keychain
   (F3.8b via fixed OS tools, no Go dependency).
4. **Redaction set is derived from resolved values**, in addition to `--redact-env`, including Base64 and
   hex and percent-encoded forms (Base64 for all three byte alignments) as defense in depth. Empty or unredactable values fail preflight instead of being
   skipped. Redaction is described as best effort, never a guarantee.
5. **Ambient-bypass closure.** A variable backing a secret reference named through `env_allowlist`,
   `environment_refs`, `env` or `input_env` of a node not assigned it is a preflight error in every mode, including the default audit mode: a
   correctness rule, not an enforcement level.
6. **Closed enforcement vocabulary:** `prevented`, `contained`, `delegated`, `detected`, `unobserved`,
   `unenforceable`. Docs, CLI and UI must never collapse them into "sandboxed". Each permission records `state`
   (what the machine can do) and `applied` (whether the mode enforces it), so an audit run never shows
   "prevented" for something that was not prevented. Decided 2026-11-17 (lead, delegated by the product owner); the team may revisit.
7. **Modes.** `audit` blocks nothing and records; `enforce` fails closed: a request that is not `prevented`,
   `contained` or `delegated` is a preflight error, and a node is never run less confined than reported. The
   runner may strengthen the mode (`--policy-mode`), a graph can never weaken it.
8. **Mechanisms (stdlib only).** Launcher checks for `shell` and `commands` (resolve once to an absolute path,
   check and launch that path). Linux Shell `paths.write` through a self re-exec helper applying Landlock at
   directory granularity; `network: off` through user plus network namespaces via `SysProcAttr.Cloneflags`.
   Agent: network and workspace confinement delegated to the Codex sandbox (pinned explicitly), path scope
   `contained` by F3.1 integration refusal and `detected` by F1.8; enforce requires `isolation: worktree` for
   Agent `paths.write`. Windows: launcher enforcement and full secret model; `paths` and `network` are
   `unenforceable`/`unobserved` (AppContainer or restricted tokens deferred to F3.9b). Both Landlock and Codex
   pinning are gated by spikes recorded under `docs/validation/` before implementation.
9. **Violation handling.** Blocked node fails with kind `policy_blocked`, a typed `policy_violation` fact and a
   red `execution_policy` Check Result; not retried by a correction loop; accepted siblings untouched. New
   facts `policy_evaluated` (carrying secret assignment names only) and `policy_violation` ride one event
   schema bump: event schema 14 (proposed; next free number confirmed at merge; F2.8b = 12,
   F3.4 = 13, Decided 2026-11-17 (lead, delegated by the product owner); the team may revisit).
10. **Ports.** Consumer-owned `runtime.SecretResolver` and `runtime.PolicyEnforcer`, composed by the CLI like
    `WorkspaceManager`. Policy evaluation and the path matcher are domain code shared with the F1.8 scope Gate.
11. **Delivery capability, fail closed.** An executor that can hand secrets to a node process declares the
    optional `runtime.SecretReceiver` (`ReceivesSecrets(nodeType) bool`). Preflight rejects a graph that assigns
    a secret to a node type the executor does not receive (`secret_delivery_unsupported`) instead of dropping
    the value. Resolved values travel to the node as a `secrets.Env` held by the run machine, not through
    `Options`, results, events or the store. Added during F3.8 implementation (C4.2); no new decision about
    scope or trust.

## Alternatives considered

### Separate secrets and policy features, each with its own preflight

Pros: smaller independent changes.
Cons: allows the ordering bug this ADR exists to prevent; two reports; two redaction sources.

### Keychain as the first provider

Pros: no secret in the runner environment.
Cons: three platform implementations before any value; the roadmap allows env first; the provider port keeps it open.

### Third-party sandbox library or container runtime

Pros: stronger, broader confinement.
Cons: new dependency and distribution weight, conflicts with local-first simplicity and the dependency policy;
reconsider for managed runners (F6.1).

### Default-deny policy

Pros: true least privilege.
Cons: breaks every existing graph and Agent adapter; recommended for v1 under enforce.

## Consequences

### Positive

- A secret cannot reach a node policy does not admit; the user consents to one report.
- Honest, per-platform statements of what is stopped, contained, delegated or only seen.
- No new dependencies; existing graphs and hashes are unchanged.

### Negative / trade-offs

- Windows has weak path and network enforcement in v0.
- Landlock helper adds a self re-exec path that needs careful hardening.
- Delegated Agent confinement depends on the vendor sandbox version.
- Reads, descendants of the node command and exfiltration by a granted process remain out of reach.

## Compatibility and migration

Graphs without `policy` or `secrets` behave and hash identically. Readers of older event schemas see no new
facts. Later versions may promote these fields to first-class schema fields with a migration.

## Validation

Acceptance criteria in both specs: byte scans of the store directory and API responses for secret values;
filesystem-level assertions that enforced writes did not happen; unenforceable requests fail preflight in enforce
mode; Windows launcher tests; architecture test for provider/platform imports; spike reports for Landlock and Codex.

## Implementation notes (F3.8)

These notes record how F3.8 implemented this ADR. They change no decision; the status stays proposed until F3.9
and the T13 acceptance.

### Scope as implemented

F3.8 ships the `env` provider and Shell nodes only: the graph surface and validation, one preflight that resolves
assigned secrets and builds the redaction set, per-node environment delivery, redaction of events, results and the
CLI/API output, and the workspace scan as an audit fact. Agent `secret_env` is rejected by validation
(`E_SECRET_ENV_AGENT_UNSUPPORTED`) until the Codex spike. Event schema 14 defines `policy_evaluated` and
`policy_violation`, but the runtime does not emit them yet; the policy stage, enforcement modes, `secret_policy_denied`
and the `policy` graph fields belong to F3.9. Decision 11 (`SecretReceiver`, `secret_delivery_unsupported`) was added
during F3.8 implementation and needed no new decision about scope or trust. Known gap: a graph literal equal to a
resolved secret is not rejected before the store copies the graph (F3.8 acceptance criterion 9).

### Run result redaction (C4.5)

The redaction set also covers the `run.Result` that `runtime.Run` returns, at one seam (`runMachine.snapshot`),
on copies only: accepted outputs and downstream inputs stay raw. Text-like outputs are masked, number, boolean
and JSON outputs are withheld rather than rewritten, and `Failure.Err` keeps `errors.Is` through a
wrapper without an `Unwrap` chain, and `errors.As` reaches only a masked copy of typed errors that implement
`Redacted(mask)` (`shell.ExecutionError`, `agent.ExecutionError`), never the raw cause. Only the first such type in the chain is exposed: types it wraps (for example
`shell.ExecutionError` under `agent.ExecutionError`) are not reachable through `errors.As`. `--redact-env` values are in the same set, so they now mask graph outputs
too. The CLI field `redacted` is additive; `cli_schema_version` stays 1. No new decision about contracts
beyond this addendum.

### Workspace scan wiring (C4.7b)

The runtime scans a granted node's changed files once, after its last attempt, and records a node-run scoped
`secret_in_workspace` Check Result. Until F3.9 it is an audit fact and never changes a node or run outcome.
Known gaps that F3.9 must close before enforcing: in a shared workspace the scan attributes neighbours' changes
(two granted nodes in one repository may both get a red result), so enforce mode must settle attribution before
blocking a node; and the red `reason` (`secret_found_in_workspace`) differs from the schema 14 fixture
(`secret_in_workspace_violation`) and is to be unified with the F3.9 policy reasons.

### Secrets must not reach an Agent over data edges

Decided by the product owner after the reviews of PR #167 and PR #169. A node's accepted output is immutable and
flows raw to its dependents; the redaction of requirement C applies to recorded copies only. A granted child can
echo its secret, and any intermediate node can pass a value on, so no Agent node may be reachable over data edges
from a node with a non-empty `secret_env`. Graph validation rejects it with `E_SECRET_FLOWS_TO_AGENT`, one error
per (source, Agent) pair on the shortest path; shell, gate, human and agent nodes are not barriers. This is a
validation rule only: no event, schema or runtime contract changes, and no new ADR. Remaining, non-edge channels
are the shared workspace (a Shell node writes a file an Agent reads; detected after the fact by the workspace scan,
not prevented) and the approval context of a human node (redacted, not rejected).
