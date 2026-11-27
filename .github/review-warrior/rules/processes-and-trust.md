---
include:
  - "internal/shell/**"
  - "internal/agent/**"
  - "internal/gitstate/**"
  - "internal/gitlocate/**"
  - "internal/proctree/**"
  - "internal/worktree/**"
  - "internal/api/**"
  - "internal/capability/**"
  - "cmd/**"
---

# Processes, paths and trust boundaries

- Commands run as structured argv. A shell string built from graph, agent or user values is a P0 or P1.
- Treat cwd, paths, environment values, graph files and agent output as untrusted. Look for path traversal, symlink escape and option injection (a value starting with `-` passed to Git or another tool).
- Environment injection is explicit and per node. Resolved secret values are redacted from events and logs (ADR-0029).
- Every child process can be terminated with its process tree (ADR-0028); check the cancellation and timeout paths, not only success.
- The API listens on loopback only and serves only granted files. Any widening of filesystem, process or network permissions is a finding unless the pull request's spec or ADR asks for it.
- Repository content is never sent to a remote service implicitly.
