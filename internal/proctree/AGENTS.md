# Process tree guidance

The root `AGENTS.md` applies here. `internal/proctree` is a neutral layer: it
imports only the standard library (and later `x/sys/windows`) and no project
package. Adapters that run children (Shell, Codex, Gate) adopt it in separate
changes; `internal/runtime` never imports it.

## Find the owner

| Concern | Start here |
| --- | --- |
| Start, Wait, idempotent Terminate, the `Prepare` hook | `proctree.go` |
| `Policy`, `Outcome`, `Method`, `LeakError` | `policy.go` |
| Unix process group, request, grace, force, verify | `proctree_unix.go` |
| Exact leaked-member count (Linux `/proc`), lower bound elsewhere | `count_linux.go`, `count_other.go` |
| Interim Windows stub (direct process only) | `proctree_windows.go` |
| Re-exec helper binary, tests | `helper_test.go`, `proctree_test.go`, `proctree_unix_test.go` |

## Preserve these contracts

- Only a tree this package created is signalled: the group id of the leader it
  started. Never look a process up by PID or name. Once the group is observed
  empty it is never signalled again, which guards group-id reuse. After the leader is reaped, a live process
  with the leader's pid is a stranger and the group is treated as gone. A
  microsecond window between that check and the signal remains on every Unix.
- `Start` keeps a caller's `SysProcAttr` and merges `Setpgid`. A `Prepare` hook
  runs before proctree sets its fields, so the hook cannot overwrite them. A
  preset `Pgid` is rejected.
- A start failure is returned unchanged (`*exec.Error`) so callers keep their
  start classification.
- `Terminate` is idempotent and safe on a nil `Tree`; its context only shortens
  the grace. A tree that stays non-empty after SIGKILL and the 1 s verify bound
  yields `*LeakError`. A descendant that left the group (`setsid`) is out of
  scope.
- Tests synchronize on a readiness line from the child, never on sleeps.
