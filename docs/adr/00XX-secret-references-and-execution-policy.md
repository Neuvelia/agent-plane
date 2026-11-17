# ADR-00XX - Secret references and execution policy v0

**Status:** proposed (number assigned at merge)
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
   `config.secret_env` (env name -> secret name) on Shell and Agent nodes; additive top-level `policy` and
   `config.policy`. No graph `schema_version` bump (as `units`). Graph defaults restrict but never grant secrets;
   a node value may only narrow a default.
3. **Secret delivery is environment-only, per node run, resolved once in process memory.** Never argv, file,
   prompt, event, store or API. First provider `env` (`env/NAME`); `local` is reserved for the OS keychain
   (F3.8b via fixed OS tools, no Go dependency).
4. **Redaction set is derived from resolved values**, in addition to `--redact-env`, including Base64 and
   percent-encoded forms as defense in depth. Empty or unredactable values fail preflight instead of being
   skipped. Redaction is described as best effort, never a guarantee.
5. **Ambient-bypass closure.** A variable backing a secret reference named through `env_allowlist`,
   `environment_refs`, `env` or `input_env` of a node not assigned it is a preflight error in enforce mode.
6. **Closed enforcement vocabulary:** `prevented`, `contained`, `delegated`, `detected`, `unobserved`,
   `unenforceable`. Docs, CLI and UI must never collapse them into "sandboxed".
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
   schema bump: next free event schema number at merge (schema 12 is reserved for F2.8b, F3.4 also proposes a
   change).
10. **Ports.** Consumer-owned `runtime.SecretResolver` and `runtime.PolicyEnforcer`, composed by the CLI like
    `WorkspaceManager`. Policy evaluation and the path matcher are domain code shared with the F1.8 scope Gate.

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
