# H12: Installed Codex adapter guidance

## Intent

An agent changing the installed Codex adapter should find the process,
protocol, progress and Git owners while preserving the provider-neutral
parent contract and current trust limits.

## Approach

Add `internal/agent/codex/AGENTS.md` beside the adapter. Ground it in
F1.2/ADR-0010, F1.3/ADR-0011, F1.8/ADR-0016 and current code and tests.
Distinguish adapter Git evidence from runtime scope checking, public
normalized progress from private provider JSONL, and process-group cleanup
from filesystem or process isolation.

This changes contributor guidance only. Product code, CLI protocol, event
schema, dependencies and CI rules remain unchanged.

## Scope and acceptance

- Every named source, test, feature, ADR and validation path exists on the
  target branch.
- The guide maps detection, environment, result, progress, Git and process
  ownership to current source and regression tests.
- It preserves explicit opt-in, structured argv, no ambient environment,
  private bounded protocol data and safe normalized observation.
- It describes invocation-relative Git evidence, ignored-path transitions
  and the distinct opt-in F1.8 scope Gate without claiming prevention,
  rollback or authorship.
- It states the Darwin/Linux process-group boundary and fail-closed behavior
  on other platforms without claiming universal containment.
- Focused Codex adapter tests and `make check` pass.

## Risks

F1.2 predates normalized progress and F1.8 scope checking; current behavior
spans three feature slices. ADR-0010 records the initially verified CLI
protocol, while `Detect` checks installed version syntax and required flags
rather than pinning one exact version. Guidance must follow that code.
