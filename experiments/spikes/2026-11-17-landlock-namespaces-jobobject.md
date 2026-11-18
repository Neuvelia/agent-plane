# Spikes: Landlock, user/net namespaces, Windows Job Objects

Date: 2026-11-17. Throwaway prototypes; nothing imports them. They de-risk F3.9
(execution policy, enforce modes) and F3.4a (Windows process-tree termination).
Code: `experiments/spikes/linuxsandbox` (Linux) and `experiments/spikes/winjob`
(Windows), both stdlib only (`syscall`, raw syscalls, `LazyDLL`); no `go.mod`
change. Each is behind a `//go:build` tag, so `go vet ./...` and `make check`
on the other OS skip it silently.

## Environments

| | Linux | Windows |
| --- | --- | --- |
| OS | Ubuntu 24.04.3 LTS in WSL2 | Windows 11 Home 10.0.26200 (25H2) |
| Kernel | 6.6.87.2-microsoft-standard-WSL2 | - |
| Go | 1.23.12 | 1.27.4 |
| User | uid 1000, no capabilities (CapEff 0) | normal user, not elevated |

Not tested: GitHub `ubuntu-latest`, Windows Server runners, macOS. Statements
about them below are reasoning, marked as such.

## Linux

### (a) Landlock: works

- `landlock_create_ruleset(NULL, 0, VERSION)` returns ABI **3** (adds REFER and
  TRUNCATE). `/sys/kernel/security/lsm` does not exist on this WSL kernel, so
  the ABI syscall is the only reliable probe. Do not rely on the lsm file.
- A re-exec helper (`linuxsandbox helper DIR... -- cmd`) locks the OS thread,
  sets `PR_SET_NO_NEW_PRIVS`, builds a ruleset that handles the write-class
  rights only (read/execute untouched), adds a path-beneath rule for the
  allowed directories, calls `landlock_restrict_self`, then `syscall.Exec`s the
  command. The domain survives exec.
- Result for `sh -c` under the helper, allowed dir A, other dir O under `/tmp`:
  write in A rc=0; mkdir in A rc=0; rm in A rc=0; write, append and mkdir in O
  all `Permission denied` (EACCES); the file in O was never created; reads of
  `/etc` unaffected (no read confinement, matches F3.9 non-goals).
- Gotchas found:
  1. Handling write rights also denies `/dev/null`: `echo x > /dev/null` failed
     until `/dev/null` was added as its own rule.
  2. A rule on a non-directory must carry file rights only (WRITE_FILE,
     TRUNCATE) or `landlock_add_rule` returns EINVAL. Directory-only rights
     must be masked off.
  3. `/tmp` and `$HOME` become unwritable, so tools that need a scratch dir
     (go build cache, git, compilers) break unless a temp dir under the
     workspace or an explicit allowance is provided. F3.9 must define a
     per-node TMPDIR and cache allowance, otherwise `paths.write: ["bin/**"]`
     on `go build` fails.
  4. Rights are ABI dependent; the helper must mask to the probed ABI
     (best-effort degradation is possible but F3.9 enforce mode should require
     a minimum ABI, 1 is enough for write containment, 3 adds truncate).
  5. `O_PATH` is not exported by `syscall`; use 0x200000 (or `x/sys/unix`).
  6. Landlock restricts the calling thread, so the helper must
     `runtime.LockOSThread` before restricting and then exec from that same
     thread. A pure `SysProcAttr` approach cannot do it; a re-exec helper is
     required (confirms the F3.9 design).
  7. Landlock does not stop writes through already-open descriptors, `connect`
     (ABI 4 adds TCP bind/connect), or reads.

### (b) User + net namespaces: works here, no AppArmor gate

- `Cloneflags: CLONE_NEWUSER|CLONE_NEWNET` with uid/gid maps `0 -> uid` size 1
  works unprivileged. Child sees uid 0 inside and a single `lo` interface that
  is **down**. A control child outside the namespace saw `lo` and `eth0` up.
- `connect` to a listener on the host `127.0.0.1:PORT`: control succeeds;
  namespaced child fails with `connect: network is unreachable` (ENETUNREACH).
  Network-off is therefore real, not advisory.
- `CLONE_NEWNET` alone fails: `fork/exec: operation not permitted`. The user
  namespace is mandatory without privileges.
- `/proc/sys/kernel/apparmor_restrict_unprivileged_userns` does not exist on
  this WSL kernel (no AppArmor), so the Ubuntu 24.04 restriction could not be
  observed. `kernel.unprivileged_userns_clone` also absent; `user.max_user_namespaces`
  is 63153. I did not and must not change sysctls.
- Reasoning, unverified: stock Ubuntu 24.04 (and likely `ubuntu-latest`, which
  is 24.04) sets `apparmor_restrict_unprivileged_userns=1`; with it, an
  unconfined binary may create the namespace but is denied capabilities inside,
  so uid map writes or the later `lo`/mount setup can fail with EPERM unless an
  AppArmor profile grants `userns`. The probe must therefore perform the real
  clone and a real connect test, not just read the sysctl.
- Untested: combining netns with Landlock (should compose, both inherited),
  and a netns child that still needs loopback (it would need `lo` brought up,
  which needs `CAP_NET_ADMIN` inside the userns via netlink; avoidable if
  the policy is simply "no network at all").

### Implications for F3.9 (enforce modes)

- `paths.write` can be `prevented` on Linux when the probe reports ABI >= 1,
  using a re-exec helper (no new dependency). Enforce mode must reserve
  `/dev/null` (and `/dev/tty` if needed) and give each node a writable TMPDIR.
- `network: off` is `prevented` where the namespace probe succeeds; the probe
  must be a real clone plus connect-refused check, bounded, cached per run.
  Where it fails (restricted AppArmor, containers without userns), the
  permission is `unenforceable` and enforce mode must stop at preflight with
  `policy_unenforceable`, as the spec says.
- Landlock cannot cover `commands` or descendants; the F3.9 non-goal stands.
- WSL2 behaves like normal Linux for both mechanisms; macOS stays unenforced.

## Windows

### (c) Job Object, KILL_ON_JOB_CLOSE: works

- Job created with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, no breakaway. Child
  (the spike binary) spawns a grandchild immediately. Assigned child and
  grandchild both reported by `IsProcessInJob` and `ActiveProcesses=2`.
  Closing the job handle: both processes gone within 2 ms, verified by holding
  process handles opened by PID. No leftover `winjob` processes afterwards.
- Exit code of a process killed by job close was **0**, not a distinctive code
  (the `TerminateJobObject` exit code is caller chosen, but close gives 0). So
  the exit code cannot classify the outcome; F3.4a rule 7 (our own record) is
  required, not optional.
- **Nested jobs:** this test process was already inside a job (the harness's);
  creating and assigning to a new job still worked (Windows 8+ nested jobs).
  W7 is expected to pass.
- **Assignment race**, 200 iterations each, child spawning a grandchild as its
  very first action:

  | strategy | escaped grandchildren |
  | --- | --- |
  | `CREATE_SUSPENDED`, assign, resume primary thread | 0 |
  | `PROC_THREAD_ATTRIBUTE_JOB_LIST` at `CreateProcessW` | 0 |
  | start normally, assign afterwards | 0 (Go startup is slower than the assign) |
  | same, with an artificial 50 ms delay before assign | 20 of 20 |

  So the unprotected strategy only appears safe by luck; the delay shows the
  window is real. Both safe strategies work. `CREATE_SUSPENDED` through
  `exec.Cmd` needs the primary thread, which Go does not expose, so the spike
  finds it with a Toolhelp thread snapshot (extra code, small TOCTOU on a fresh
  PID). `JOB_LIST` needs raw `CreateProcessW` with `STARTUPINFOEX`, bypassing
  `os/exec`, so stdio, env and `Wait` plumbing must be reimplemented.
  Recommendation for W2: prefer `CREATE_SUSPENDED` via `exec.Cmd.SysProcAttr`
  plus a thread-resume helper (keeps `os/exec`), or `JOB_LIST` if the project
  accepts owning process creation on Windows; `JOB_LIST` has no window at all.
  `JOB_LIST` needs Windows 10 / Server 2016+ (already the F3.4a floor).
- Stdlib `syscall` was sufficient for the spike (LazyDLL for the rest);
  `golang.org/x/sys/windows` would only reduce boilerplate.

### (d) CTRL_BREAK to a process group

- Child started with `CREATE_NEW_PROCESS_GROUP`; `GenerateConsoleCtrlEvent(
  CTRL_BREAK_EVENT, childPid)` succeeded and Go's `signal.Notify(os.Interrupt)`
  in the child received it (CTRL_BREAK maps to os.Interrupt); child exited 0.
  The harness had a console (a hidden one; `GetConsoleWindow` was 0).
- With the runner started `DETACHED_PROCESS` (no console): the call returned
  `The handle is invalid`, the child was not asked to exit, and its grandchild
  stayed alive. This confirms F3.4a: graceful is best effort, and no-console
  must fall through to forced termination after the short wait (W5).
- Without a job, the grandchild survives a graceful stop as well, which is why
  the job is the guarantee and CTRL_BREAK only a courtesy.

### Implications for F3.4a W-tests

- W1, W2, W4, W7 feasible as specified with a self-built helper binary (no
  `ping`/`cmd` needed). W3 needs our own cancelled record (exit code is 0).
- W5 is reproducible with `DETACHED_PROCESS`; W6 mirrors the helper approach.
- Test helpers should hold process handles from creation (or open by PID right
  after the ready marker) to avoid PID reuse in liveness checks.

## CI implications

- `check (windows-x64)`: Job Object tests need no privileges and work inside
  an existing job; safe to run on the GitHub Windows runner (not tested there).
  The detached-console case needs the test to spawn itself with
  `DETACHED_PROCESS`.
- `ubuntu-latest`: not tested. Landlock should be available (kernel >= 5.13
  with the LSM enabled) but must be probed, not assumed. The netns tests may be
  skipped or expected-failed if AppArmor restricts user namespaces; F3.9 tests
  should assert `unenforceable` plus the preflight error in that case, and
  assert real denial where the probe passes. Never relax the sysctl in CI as
  part of product tests; if a job needs it, do it in the workflow, explicitly.
- Add a CI step that prints the probe output (Landlock ABI, userns result,
  kernel) so the first run on `ubuntu-latest` documents the real values.
- `make check` is unaffected: spikes are build-tagged per OS and import no
  product package.

## How to rerun

```
go run ./experiments/spikes/linuxsandbox probe|landlock|netns   # Linux
go run ./experiments/spikes/winjob tree|nested|ctrlbreak|detached
go run ./experiments/spikes/winjob race suspended|joblist|late 200
SPIKE_ASSIGN_DELAY_MS=50 go run ./experiments/spikes/winjob race late 20
```
