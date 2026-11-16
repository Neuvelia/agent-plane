# H14: Gate-local agent guidance

## Intent

An agent changing deterministic checks should find the config, evaluation,
command and Git-scope owners while preserving the difference between a valid
red result and an operational error. Root, runtime, events and Gitstate
guidance remain in force at their boundaries.

## Approach

Add `internal/gate/AGENTS.md` beside the Gate code. Ground it in F1.4,
F1.5, F1.8 and their ADRs, plus current tests. Describe four check kinds,
restricted read-only command execution, typed Check Results and the
runtime-owned observation/trace path. Distinguish reported paths from
Git-observed changed-file scope.

This changes contributor guidance only. Product code, graph/event schemas,
dependencies and CI rules remain unchanged.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide maps config, predicate, reported scope, fixed diff and Git scope
  to current owners and regression tests.
- It preserves green/red/error semantics, declared data bindings, bounded
  evidence, typed result v1 and runtime-owned event correlation.
- It describes fixed diff as a narrow read-only operation without claiming
  a race-proof filesystem or general process sandbox.
- It distinguishes reported-file claims from Git observations and warn/deny
  scope decisions without claiming prevention, rollback or authorship.
- Focused Gate tests and `make check` pass.

## Risks

Older F1.4 docs describe the initial minimal Gate fact; F1.5 now provides
the full versioned CheckResult, while schema-4 records remain historical.
The Gate package constructs verdicts, but runtime validates and commits
their event facts. Those responsibilities must not be merged in guidance.
