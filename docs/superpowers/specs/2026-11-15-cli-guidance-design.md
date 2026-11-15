# H10: CLI-local agent guidance

## Intent

An agent changing the CLI or sidecar composition should find command owners,
public exit/JSON contracts and cross-package test coverage. The root and
relevant package-local `AGENTS.md` guides remain in force.

## Approach

Add `cmd/agentplane/AGENTS.md` beside the entrypoint. Ground it in current
command code, F0.7/ADR-0007, F1.3/ADR-0011, F2.9/F2.5/ADR-0019 and the
recent shared validation path. Distinguish CLI presentation and concrete
composition from graph, runtime, event, store and API ownership.

This is contributor guidance only. Product behavior, CLI JSON, event schema,
dependencies and CI rules do not change.

## Scope and acceptance

- Every named source, test, feature and ADR path exists on the target branch.
- The guide maps command routing, diagnostics, inputs, output, JSON,
  executor/decision composition, history and serve setup to current files.
- It describes current bounded watch delivery rather than relying on the
  historical synchronous delivery text in ADR-0007.
- It preserves exit codes, CLI schema 1, validation parity, secret checks,
  opt-in CLI history and server write-before-delivery behavior.
- It distinguishes explicit Codex execution in `run` from the current
  server's lack of an Agent adapter and `--redact-env` flag, and does not
  equate a successful store append with fsync of progress.
- It points to focused command, built-binary and API parity tests.
- Focused CLI tests and `make check` pass.

## Risks

The CLI is an integration boundary with multiple feature eras. Some older
ADRs remain valid for exit/JSON contracts but have delivery details superseded
by ADR-0011. F2.4 E3.1/E3.2 are current; its remaining feature checklist is
not a blanket claim of complete product behavior.
