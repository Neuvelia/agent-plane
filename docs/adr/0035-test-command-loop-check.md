# ADR-0035 - Test command as a loop check

**Status:** proposed
**Date:** 2026-11-26

## Context

F1.4 AC1 and the Phase 1 exit criterion expect a failing test to produce a red
verdict that blocks downstream work and, in a loop, feeds a correction. ADR-0012
deliberately limited the Gate `command` kind to a fixed, read-only
`/usr/bin/diff`, rejecting "arbitrary commands called read-only" because a
writable test runner cannot satisfy a read-only invariant without real OS
containment (ADR-0012, Alternatives). ADR-0018 made a non-zero Shell exit fatal
and not retried. Together the two left no way to express
`check: {type: shell, run: pytest}` (architecture plan sections 5.7 and 11), and
the real F1.7 demo had to use a diff Gate instead of a test (audit D12, risk 3).

## Decision

Add a Gate kind `test_command` (Check Result kind `test_command`) that runs an
author-chosen command as a check.

Semantics:

- Exit status 0 is green. Any other exit status is red with one evidence item
  holding the exit code and bounded, redacted stdout/stderr with omitted-byte
  counts (the 8 KiB per stream limit and redaction path of ADR-0012). A red
  result is a valid Check Result, so in a loop it is exactly the red verdict of
  ADR-0014: the producer is re-run (an Agent receives the Check Result as
  correction feedback; a Shell producer is re-run unchanged with
  `AGENTPLANE_ATTEMPT`).
- Start failure, death by signal (no exit status), cancellation, a timeout and an
  unusable cwd are operational errors and never a verdict, as for the diff
  command. A hung runner is bounded by a hard limit (10 minutes, in addition to
  any node `timeout_seconds`).
- A Shell producer's own non-zero exit stays fatal and is not retried
  (ADR-0018, `TestShellLoopNonZeroExitIsFatalNotRetried`). Only the check's exit
  status maps to red. Non-check Shell nodes and the diff `command` kind are
  unchanged.

Configuration (static, side-effect free to validate): `check_id`, `kind`,
`command` (nonempty, run as argv without a shell), `args`, `cwd` (canonical
absolute), `env_allowlist`, `env`, `side_effects` and `input`. The command,
args, cwd and environment are literal: bound inputs never reach them. `input`
names a required string or JSON port and is the checked input; it exists so the
check is a real data dependency on the candidate. The child environment is empty
except `env_allowlist` entries present in the runner and explicit `env` values;
`AGENTPLANE_ATTEMPT` is rejected as for Shell nodes.

### Side-effect and read-only policy

`test_command` is not read-only and the Gate does not claim it is. A test runner
may write caches, build output, reports or workspace files. The policy is:

1. **Declared, required, never defaulted.** `side_effects` must be stated:
   `none` (the author expects the command to write nothing) or
   `workspace_writes` (it may write inside the workspace). The declaration is
   recorded in the Check Result reason (for example
   `command exited 1; declared side_effects: workspace_writes`) so the trace shows
   what the author accepted. It is not enforced, verified or used to sandbox.
2. **Loop only.** A `test_command` Gate is valid only as a loop's embedded Gate,
   for Shell and Agent producers. A standalone Gate node with this kind is
   rejected by graph validation: a standalone Gate is documented as read-only
   evaluation (ADR-0012), and a loop already accepts persistent side effects of
   rejected attempts (ADR-0018). The desktop does not offer it standalone.
3. **No isolation claimed.** Side effects of the check persist across attempts.
   There is no rollback, no filesystem or network confinement and no resource
   quota; a diff-based or scope Gate must not be assumed to see changes the test
   command makes. Process-tree termination follows `internal/proctree` and the
   platform limits documented there.
4. **No input interpolation; secrets follow ADR-0029.** The embedded Gate
   cannot be assigned secrets (`secret_env` on a loop gate is rejected), so a
   test command never receives a secret value. Its `env_allowlist` and `env`
   names count as channels for the ambient-bypass preflight: naming the
   variable behind a secret reference is `E_SECRET_AMBIENT_BYPASS`, as for any
   node not assigned that secret. Evidence is redacted with the run's redaction
   set like every Check Result; the command is untrusted author configuration.

The existing `command` kind keeps its fixed read-only diff contract unchanged.

## Alternatives considered

### Alternative A - allow arbitrary commands under kind `command`

Pros: no new kind.
Cons: would silently break the read-only contract of ADR-0012 for the diff
Gate and for every consumer that reads the kind as "read-only comparison".

### Alternative B - treat a non-zero producer exit as red

Pros: no new Gate kind.
Cons: conflates a crashed/misconfigured process with a failing check, contradicts
ADR-0014/ADR-0018 and the non-retry guarantee; the producer would also have to
be the test.

### Alternative C - enforce read-only with an OS sandbox

Pros: honest read-only claim.
Cons: platform-specific dependency and large scope (ADR-0012 already rejected
it); belongs to the execution policy track (F3.9).

### Alternative D - default or omit `side_effects`

Pros: shorter config.
Cons: hides that the check can write; the explicit declaration is the only
policy surface this decision can honestly offer without containment.

## Consequences

### Positive

- A failing test is a red verdict that blocks downstream work and drives the
  correction loop for Agent and Shell producers (closes the F1.4 gap).
- Failure evidence (exit code, bounded output) reaches the agent as feedback.

### Negative / trade-offs

- A new Check Result kind: older readers reject `test_command` results (they
  validate kinds); the event schema number is unchanged by this ADR because the
  Check Result shape is additive (the schema owner records the new kind with the
  next schema bump).
- Side effects of the test command are unconstrained and unobserved.
- `side_effects` is advisory.

## Compatibility and migration

Graph schema 0 is additive: a new Gate kind valid only in a loop. Existing
graphs, the diff `command` kind and non-check Shell semantics keep behavior and
semantic hashes. Check Result v1 gains kind `test_command`; the evidence exit
code may be any non-negative status for this kind (0 and 1 only for `command`).

## Validation

Gate tests for green, red, bounded evidence, environment, timeout, start failure,
cancellation and configuration rejection; field-descriptor parity; CLI loop
tests (red then green, exhaustion, producer failure stays fatal, unstartable
runner is not red, standalone rejected); `make check`.
