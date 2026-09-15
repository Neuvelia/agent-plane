# Testing strategy

## Testing pyramid for the execution core

### Unit tests

Use for:

- schema decoding;
- validation rules;
- binding resolution;
- state transitions;
- event construction;
- pure scheduler decisions.

### Package integration tests

Use for:

- scheduler plus fake executors;
- process lifecycle;
- stdout/stderr capture;
- cancellation;
- temporary workspace behavior.

### CLI integration tests

Use sparingly for:

- exit codes;
- JSON output contract;
- actionable validation errors;
- end-to-end example graph execution.

## Concurrency tests

Do not use arbitrary sleeps to prove concurrency if synchronization can be explicit.

Prefer test executors that expose channels/barriers so tests can prove:

- A and B can both enter running state before release;
- merge cannot start before both complete;
- failure blocks only the required downstream path.

## Failure-first cases

Every execution feature should test at least one failure mode.

Examples:

- malformed graph;
- missing node reference;
- cycle;
- command exits non-zero;
- child process cannot start;
- context cancellation;
- invalid binding;
- terminal state reached twice.

## Golden files

Use golden files only when they clarify stable user-facing serialization/diagnostics. Avoid broad snapshots that make semantic regressions hard to see.

## Temporary resources

Tests must use temporary directories/repositories and clean them automatically.

Never depend on the developer's real repository state, user home contents, or global credentials.
