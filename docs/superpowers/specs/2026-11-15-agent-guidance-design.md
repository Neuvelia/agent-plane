# H11: Provider-neutral Agent guidance

## Intent

An agent changing the provider-neutral adapter boundary should find the
config, dispatch, result and conformance owners while preserving the runtime
dependency direction. Root `AGENTS.md` and runtime/event guides remain in
force.

## Approach

Add `internal/agent/AGENTS.md` beside the neutral contract. Point to F1.1,
ADR-0009, current progress/correction extensions and tests. Separate this
package's validation and conversion from provider-specific Codex process,
Git and environment behavior, which will receive a narrower child guide.

This changes contributor guidance only. Product code, graph/event schema,
adapter interfaces, dependencies and CI rules remain unchanged.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide maps contract, config, executor, errors, mock/conformance and
  concrete adapter boundaries to their current files.
- It preserves explicit adapter lookup, nil-versus-false capabilities,
  task-source validation, scheduler-owned correlation and bounded progress.
- It distinguishes advisory paths and references from filesystem isolation,
  secret resolution and changed-file scope checking.
- It describes declared output ports, nil-versus-empty values, separate
  evidence and event-safe errors accurately.
- Focused Agent tests and `make check` pass.

## Risks

Older F1.1 documentation says Agent config errors surface only at execution
preflight; current CLI/API static validation also calls the side-effect-free
`ValidateNodeConfig`. The guide must follow current code without changing
that behavior. The real Codex adapter has additional process and Git rules
outside the neutral package scope.
