# H15: Shell-local agent guidance

## Intent

An agent changing Shell execution should quickly find the process, config and
test owners while preserving explicit argv/environment handling, typed failure
evidence and the runtime-owned loop and event boundaries.

## Approach

Add `internal/shell/AGENTS.md` beside the executor. Ground the guide in F0.5,
F1.6a, their ADRs and current tests. Distinguish raw process capture from
runtime-redacted log events, and a valid red Gate retry from a fatal Shell
failure.

This changes contributor guidance only. Product code, graph/event schemas,
dependencies and CI rules remain unchanged.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide maps config, process lifecycle, output conversion, logging and
  Shell-loop behavior to the current owners and regression tests.
- It preserves explicit argv, empty-by-default child environment, opt-in input
  mapping, failure evidence, cancellation and one-second pipe-drain semantics.
- It distinguishes unbounded raw capture and accepted output values from
  redacted log events, and does not claim descendant termination, isolation or
  rollback.
- It describes the reserved attempt variable and fatal nonzero exit without
  implying that a Shell loop receives correction feedback.
- Focused Shell tests and `make check` pass.

## Risks

ADR-0005 predates the later input mapping and Shell-loop contract. Read it
alongside ADR-0008 and ADR-0018. `ExecutionError.Result` carries raw evidence;
the runtime event boundary applies configured redaction separately. Guidance
must keep those facts distinct.
