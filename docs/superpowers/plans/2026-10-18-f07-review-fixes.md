# F0.7 review fixes

## Current state

F0.7 parses, validates, schedules, executes, emits events, and returns JSON and
process status. The Phase 0 example routes values through bindings but Shell
execution ignores them, so its fixed output does not prove end-to-end data flow.
The examples assume `printf`, and JSON serialization errors return status 1
without a diagnostic.

## Scope and contracts

Add one explicit Shell configuration field, `input_env`, mapping declared
string node input names to child environment variable names. It does not parse
expressions or coerce values. Mapped variables are added only when the bound
input is present. Reject undeclared or non-string inputs, invalid or duplicate
variable names, and conflicts with `env` or `env_allowlist` before launching a
process. Existing graphs without `input_env` keep their behavior. Document the
durable graph configuration change in the graph spec, F0.5 spec, and an ADR.

## Implementation steps

1. Add Shell executor tests for mapped bound values, missing/invalid mapped
   values, and configuration conflicts. Run the focused test first and observe
   failure; implement the minimal `input_env` handling in `internal/shell`.
2. Replace the Phase 0 fixture's fixed commands with a portable, repository
   supplied Go helper. Add a built-CLI test showing that changing a graph input
   changes both branches and the dependent final output, while event sequence
   proves branch overlap and dependency enforcement.
3. Replace platform-dependent successful CLI test fixtures with a portable
   helper process; retain platform-specific signal tests where necessary.
4. Add a failing JSON writer test, then report serialization/write failures on
   stderr with exit 1. Keep machine-readable stdout exclusive to JSON.
5. Update README and contracts. Run focused tests, `make check`,
   `go test -race ./...`, build and smoke-run the CLI, and inspect the diff.

## Risks and edge cases

- Environment names must be unique; no implicit inherited environment.
- Optional unmapped inputs stay absent. Required bound values are enforced by
  the binding layer; direct executor callers receive an error if a required
  mapped input is absent.
- The example uses `go run` and therefore needs the Go toolchain, which this
  Go repository already requires for building and testing.
- Redaction remains an explicit F0.6 log option and does not change graph
  output values.
