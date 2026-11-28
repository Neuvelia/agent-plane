# CLI process helpers guidance

The root `AGENTS.md` and parent `internal/agent/AGENTS.md` apply here. This
package holds provider-neutral helpers shared by CLI-backed adapters
(`codex` today). It imports only the standard library and `internal/proctree`,
never `internal/agent` or a concrete provider, so every adapter can use it
without importing another adapter (R2).

## Find the owner

| Concern | Start here |
| --- | --- |
| Child launch in an owned process tree, cancellation and failure classification on Darwin/Linux | `process_unix.go` |
| Fail-closed behavior on other platforms | `process_other.go` |
| `Spec`, `Stage`, classified `Error` | `run.go` |
| Bounded capture buffer | `buffer.go` |
| Duplicate-key, UTF-8 and JSON-depth check | `json.go` |

## Preserve these contracts

- `Run` starts the child only through `internal/proctree`, with stderr
  discarded and a bounded wait delay. The caller owns argv, environment and
  stdin; this package never forwards ambient environment by itself. On
  cancellation the tree is terminated (request, grace, force) and the failure
  is classified from that record, not from the exit status.
- `Run` returns `*Error` with a `Stage` for start, shutdown, cancellation and
  unsupported platforms. It carries no provider wording or diagnostic codes;
  the adapter maps `Stage` to its own typed error and copies `Termination`.
  Any other failure is the child's wait error.
- `LimitedBuffer.Write` always reports the full length so `io.Copy` is not
  cut short; callers must check `Overflow`. `UniqueJSON` rejects invalid
  UTF-8, duplicate keys, nesting beyond 64 and trailing values.
- Keep Codex-, Claude- or any provider-specific flags, parsing and messages
  out of this package.

Run `go test ./internal/agent/...` (process tests need Darwin/Linux) and
`go test -race ./internal/agent/cliproc/...` after process changes. Update this
guide when a named owner or contract changes.
