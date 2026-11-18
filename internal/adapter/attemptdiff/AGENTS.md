# Attempt diff capture guidance

The root `AGENTS.md` applies here. This file adds rules for
`internal/adapter/attemptdiff/`, the F2.8b adapter behind opt-in `attempt_diff`
events (ADR-0026, `docs/features/F2.8-attempt-compare.md`). It observes a Git
workspace and compares two observations. It does not decide when to capture,
the run budget, event order or acceptance: the runtime owns those and capture
never changes an execution outcome.

## Find the owner

| Concern | Start here |
| --- | --- |
| Open/Close, per-file observation, text/binary/size decisions | `snapshot.go` |
| Allowlisted Git calls (`ls-files`, `cat-file blob`) | `git.go` |
| Private scratch copies (0700/0600, SHA-256 keyed, 64 MiB cap) | `scratch.go` |
| Snapshot comparison, redaction order, budgets, list cap, summary | `compare.go` |
| Typed errors carrying a closed diagnostic code | `errors.go` |
| The line diff and patch rendering | `internal/domain/diff` |

## Preserve these contracts

- Start Git only through `gitstate.Run`, only for `ls-files`, `cat-file blob <id>`
  and (via `gitstate`) `rev-parse`. Never `diff`, `status`, `add`, `stash` or
  anything that writes; `Recorder.run` refuses other subcommands. Never include
  Git stderr, absolute paths or repository text in an error.
- Never write into the repository or `.git`. Before-content that Git cannot
  return (dirty, eol/filter divergence, untracked) goes to scratch; `Close`
  removes it.
- Redact both sides' text and the path before diffing (Q4); hashes and sizes
  describe the original bytes. Ignored files are `Lstat`-ed, never opened.
- Open files with the Lstat/Fstat `SameFile` check, never follow symlinks and
  confine every path with `gitstate.ConfinedPath`. An entry that cannot be
  observed is `unreadable`, never fatal.
- Bounds: 1 MiB per side, 16 KiB patch per file, 256 KiB per attempt,
  256 listed files, 20,000 ignored entries, 64 MiB scratch, 60 s per snapshot.

Tests use temporary Git repositories only. Run `go test -race ./internal/adapter/attemptdiff`.
