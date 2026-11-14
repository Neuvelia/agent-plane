# H2: Runtime-local agent guidance

## Intent

An agent changing `internal/runtime` should find the owning file, the relevant
contract, and the right regression tests without rediscovering the scheduler's
state and event rules. The root `AGENTS.md` remains the repository-wide guide.

## Approach

Place one concise `AGENTS.md` in `internal/runtime`. It applies to the Go
runtime package and its tests. It maps responsibilities to current files,
states the few package-specific invariants most likely to be broken, and points
to existing specs, ADRs, and test groups. It does not restate generic Git,
planning, review, or `make check` policy.

Alternatives considered: extending the root guide would make every contributor
read runtime-specific detail; a separate `docs/` guide would be easier to miss
when an agent enters the package. A local instruction file puts the guidance at
the change site with no tool or dependency.

## Scope and acceptance

- Only the local instruction file and its design/plan documents change.
- Every named source, test, spec, and ADR path exists on the target branch.
- Guidance distinguishes coordinator-owned state and accepted values from
  worker observations, including human approval events.
- Guidance keeps loop acceptance, retry, event order, cancellation and shared
  workspace limitations consistent with the current contracts.
- An independent reviewer can use the guide to locate files and checks for a
  scoped retry change without relying on the implementer's explanation.
- `make check` passes. No product behavior, public contract, dependency, or CI
  rule changes.

## Risks

Instructions can drift when files move or contracts evolve. Keep file pointers
short and require updating this guide with the contract it describes. Do not
turn the guide into a duplicate feature spec or imply that Git-observed scope
checking isolates writes.
