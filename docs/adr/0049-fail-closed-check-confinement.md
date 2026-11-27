# ADR-0049 - Fail-closed confinement for test_command checks

**Status:** proposed
**Date:** 2026-11-27
**Feature:** F3.9d (issue #205)
**Amends:** ADR-0029 / F3.9 v0 scope (adds read masking of a fixed credential list); ADR-0042 (an opt-in confined mode)

## Context

ADR-0042 lets a loop Gate run an author-chosen test command with the runner's
privileges. It records this as a declared, unenforced side-effect policy. In
practice the command executes code an Agent has just produced. F3.9 v0 plans
`network: off` (user and network namespaces) and `paths.write` (Landlock
through a re-exec helper) for Shell nodes, but read confinement is explicitly
out of its scope, and Gates are not covered.

The reference project of the adoption analysis
(`kacwojdev/agent-control-plane`, `docs/security-review-2026-10.md`) treats a
fail-closed check sandbox as its central guardrail: no network, scrubbed
environment, tmpfs over credential directories, and a selftest as the
container healthcheck. Users relying on that cannot adopt a tool that runs
checks unconfined.

## Decision

1. **`confinement: required` on a `test_command` Gate selects one fixed
   profile.** The default is `none`; graphs without the key are unchanged.
2. **The profile on Linux:**
   - a new user and network namespace (F3.9 FR14);
   - Landlock write rules for the Gate `cwd` tree and a private `TMPDIR` only
     (the F3.9 FR13 helper);
   - a new mount namespace in which each existing entry of a fixed list under
     the runner's `HOME` is masked. A directory gets an empty read-only tmpfs;
     a file gets a read-only bind of `/dev/null`;
   - `PR_SET_NO_NEW_PRIVS`;
   - the environment rules of ADR-0042, unchanged.

   The list is fixed in the F3.9d spec. Changing it requires amending this
   ADR.
3. **Fail closed.** Preflight probes each element. Any element `unenforceable`
   on this machine means the node fails before `run_started`
   (`E_CONFINEMENT_UNAVAILABLE`, naming the mechanism and the remedy). The
   helper verifies each element after applying it and refuses to `exec` on any
   failure. There is no degraded or audit mode for `required`.
4. **One helper.** The confinement helper is the F3.9 helper, extended with
   the mount namespace step. Whichever feature lands first creates it with the
   F3.9 FR13/FR14 semantics, so there is never a second launcher.
5. **Honest vocabulary.** The profile is reported with the F3.9 states
   (`prevented` / `unenforceable`) and never as "sandboxed". The capability
   report gains `check_confinement`, which is `prevented` only after a passing
   selftest.

## Alternatives considered

### A. Wait for F3.9 and express the same via a general policy

Pros:

- One surface.

Cons:

- F3.9 v0 excludes read confinement and Gates.
- The general policy language is larger and later. Adoption is blocked until
  then.

### B. Container or VM per check (Docker, Firecracker)

Pros:

- Stronger isolation.

Cons:

- A runtime dependency and an installation burden.
- Not local-first by default.
- Slower.

Can be offered later as F3.12 (Docker) integration.

### C. Configurable mask list and profile in the graph

Pros:

- Flexible.

Cons:

- A policy language in the graph, ahead of F3.9.
- Authors could weaken it per graph.
- Review burden.

### D. Mask the whole `HOME`

Pros:

- Simple and strong.

Cons:

- Breaks most toolchains (caches, Go module cache, npm, pip, cargo) and makes
  checks slow or failing.

## Consequences

### Positive

- Checks of agent-written code cannot reach the network, write outside the
  worktree or read the common credential stores, or they do not run at all.
- Adopting users keep the guardrail they rely on.

### Negative / trade-offs

- Linux only. macOS and Windows users cannot use `required` until F3.9b or a
  later decision.
- Distributions that restrict unprivileged user namespaces (for example
  through AppArmor) need a sysctl or profile change. Docs must say so.
- A fixed list can miss a tool's credential location. The list is
  conservative and amendable by ADR.
- Toolchains that need the network during tests (dependency download) fail.
  Authors must vendor dependencies or not use `required`.

## Compatibility and migration

Additive. No change to existing Gates, hashes or events. The reason text of
confined checks gains a suffix.

## Validation

F3.9d acceptance criteria on a Linux runner with user namespaces, and a
recorded run on a distribution where they are restricted, showing the
fail-closed preflight error.
