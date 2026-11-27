# ADR-0041 - Workspace isolation for Shell nodes

**Status:** proposed
**Date:** 2026-11-26
**Feature:** F3.1c (`docs/features/F3.1c-shell-workspace-isolation.md`); extends ADR-0024 (accepted, Agent scope only)
**Closes:** issue #189 (owner decision Q17: a gap until an ADR decides)

## Context

Roadmap F3.1 scope is a worktree or isolated checkout per node or unit so parallel units and retries do not
overwrite each other. Implemented: `isolation: worktree` is opt-in and applies only to loop-enabled Agent nodes
listed in `units` (`internal/runtime/isolation.go`, `isolatedNodes`). Shell, Gate and Human nodes are not
isolated. ADR-0024 decision 5 and F3.1 Q4 defer Shell isolation to a follow-up without an ID.

Facts:

- A Shell node's `cwd` is a config string, absolute or relative to the runner's working directory
  (`graph-spec.md`); it is not derived from a `workspace` field like Agent.
- Shell loops (F1.6a, ADR-0018) produce attempts that write into the shared checkout, the same hazard F3.1 solved
  for Agents: a parallel sibling or a retry can overwrite files.
- ADR-0024 integration is triggered by the F1.7 accept-once transition over `units`; Shell loop producers with
  Return Envelopes are enrolled in the same mechanism (ADR-0018), to be confirmed in implementation.
- A worktree is not a sandbox (F3.9 owns confinement); a command with an absolute path still writes outside.
- Gate nodes read files at a path; a standalone Gate observes the source checkout and so does not see integrated
  results (ADR-0024 decision 5). That downstream visibility problem is F3.1b, not this ADR.

## Decision (recommended; the owner chooses)

Recommended option **B**: allow `config.isolation: worktree` on **loop-enabled Shell producer nodes listed in
`units`**, reusing the ADR-0024 mechanism unchanged (per-node linked worktree, deliver as refs, serial
deterministic integration, conflict means `needs_intervention`). Standalone Shell, Gate and Human nodes remain
unisolated. Downstream visibility of integrated files stays F3.1b.

1. Same constraints as Agent: base SHA at run start, dirty-source preflight refusal, untracked files not carried,
   unknown filter drivers rejected, controlled Git invocation, hooks never run.
2. **cwd mapping.** For an isolated Shell node, `cwd` must resolve inside the source repository root. The runtime
   remaps it to the same repository-relative path inside the node's worktree. A `cwd` outside the repository, or a
   non-existent mapped directory, fails preflight before `run_started`. Relative `cwd` is resolved against the
   source root first, then remapped.
3. The embedded Gate of an isolated Shell loop evaluates that node's worktree, as for Agent.
4. `command` and `args` are not rewritten. An absolute path in `args` is a documented escape (worktree is not a
   permission boundary); F3.9 `paths.write` with `isolation: worktree` is the confinement route and F3.9's
   isolation requirement already names isolation as the contained mode.
5. Isolation on any other Shell shape fails preflight with a message naming the node and the requirement.

## Alternatives considered

### A. Keep Shell unisolated and document

Pros: no work; Shell commands are deterministic and author-controlled, hazard limited to parallel siblings that
write the same file.

Cons: roadmap F3.1 wording unmet; correction loops over Shell still overwrite the shared tree; F3.9 `paths.write`
for Shell would have no contained mode.

### B. Isolate loop-enabled Shell producers in `units` (recommended)

Pros: reuses the proven mechanism and event shapes; smallest change; closes the retry/parallel hazard for the
nodes that actually write files; `cwd` mapping is explicit and testable.

Cons: `cwd` remapping is a new rule to explain; standalone Shell nodes (for example a test runner after workers)
are still not isolated and still cannot see integrated files (F3.1b); some events need the node type widened.

### C. Isolate every Shell node, including standalone ones, with an ephemeral worktree at the integrated tip

Pros: gives "workers then test" pipelines the integrated result.

Cons: that is F3.1b semantics (base = base + accepted upstream) with its merge-before-start and conflict paths;
mixes two decisions; much larger consequence analysis. Better as a separate decision.

### D. Process or container sandboxing for Shell

Out of scope: F3.9 and later.

## Consequences

### Positive

- Shell producers get the same no-race, no-leak-of-rejected-attempts guarantees as Agents.
- A named target (F3.1c) replaces the unnamed deferral in F3.1 and ADR-0024.

### Negative / trade-offs

- Windows: worktree isolation is Linux and macOS only (F3.1). F3.1c inherits this; the Windows test posture
  matches F3.1.
- Users must understand two caveats: not a sandbox, and standalone consumers do not see integrated files.

## Compatibility and migration

Additive: `isolation` already exists in the Agent field registry; allowing it for Shell adds a registry entry and
a validation rule. No schema bump (`config` is an open per-type mapping; confirm with graph owner), no new event
types if the existing isolation events carry the node type. If an event field must be added, it takes the next
event schema number owned by the schema-numbering owner (issue #84 wave), not chosen here. Existing graphs are
unaffected: absent `isolation` means `none`.

## Validation

F3.1c acceptance criteria: two parallel isolated Shell producers write the same file without a race, a rejected
Shell attempt never reaches the integrated ref, `cwd` remap and escape failures are tested. Not implemented by
this ADR.
