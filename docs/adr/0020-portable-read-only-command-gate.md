# ADR-0020 - Portable read-only command Gate

**Status:** accepted
**Date:** 2026-10-21

## Context

The F1.4 `command` Gate accepts only the trusted Darwin/Linux executable
`/usr/bin/diff`. It compares two preopened files through `/dev/fd/3` and
`/dev/fd/4`. This graph value is durable and existing graphs must keep its
behavior. Windows has neither that executable nor those descriptor paths.
The current path preflight also cannot prevent a concurrent replacement between
inspection and open. F2.0 needs a deterministic local file comparison on each
supported desktop OS without permitting an arbitrary graph command.

## Decision

Add the exact Gate config value `command: agentplane.diff`. It is an internal
bytewise comparison, not a process name or a lookup on `PATH`. Its only inputs
are the existing required string ports named by `left_input` and `right_input`
and the existing canonical absolute `cwd`. No args, environment, options, or
alternate command names are accepted. The output contract stays the declared
`verdict: string` and the schema-5 `gate_check` fact. Identical observed bytes
are green; different observed bytes are red; a missing, inaccessible, unsafe,
or canceled input is an error. The existing `command` evidence item is used
without a schema change: `source: command`, `exit_code: 0` for equal or `1`
for different, and empty stdout/stderr with zero omitted bytes. In a `command`
Gate result, `exit_code` is the fixed comparison status 0/1. For the legacy
`/usr/bin/diff`, that status also equals its actual child exit; for
`agentplane.diff`, no child process runs. Trace readers and UI must label the
field as comparison status, not infer that a process exited. No file contents
or new metadata are emitted.

Open `cwd` from the filesystem root with a no-follow, handle-relative walk:
reject every symlink or Windows reparse point in `cwd` and its ancestors,
require the final handle to be a directory, and pin its identity while each
input is opened. Detect and reject a root replacement observed during the
walk or before the input handles open. Operand components are opened relative
to pinned parent handles; if an already opened subtree is renamed afterward,
comparison may observe its old opened files rather than the current pathname.
The pinned walk prevents that rename from escaping the selected root but does
not provide a path snapshot. A replacement already complete before
the first open cannot be distinguished from the current path without a prior
trusted directory identity; callers needing that guarantee must supply a
stable selected workspace snapshot. Open each
relative input read-only through a platform-specific walk rooted at that
handle. Reject symlinks/reparse points in every component and require the final
handle to identify a regular file. Compare opened handles in bounded chunks,
checking cancellation before each read and before returning a verdict. An
underlying filesystem read that never returns (for example on a stalled FUSE
or network mount) can delay cancellation; the in-process comparator cannot
interrupt that kernel operation. Such mounts require an out-of-process Gate
worker before they can be claimed as promptly cancelable. Recheck handle
identity/size/metadata after the read
and report detected mutation as an error. Same-size in-place writes or
mutate-and-restore can evade those checks: the verdict describes the bytes
observed from the opened handles, not a guaranteed atomic snapshot. A workspace
that may be concurrently rewritten by an adversary cannot use this check as a
strong integrity gate; obtain a stable copy through a separate mechanism first.
Any platform that cannot enforce safe path traversal fails closed.
The first Windows implementation accepts canonical local drive-absolute cwd
paths only. UNC, device namespace and alternate-data-stream paths fail with an
explicit diagnostic until their confinement and identity rules are specified
and tested natively.

Keep `command: /usr/bin/diff` on Darwin/Linux with its exact existing argv,
descriptor, locale, exit, and evidence behavior. On Windows, this legacy value
remains valid for parsing old YAML but execution returns an explicit unsupported
error. Run its process through the desktop-owned child lifecycle so sidecar
death cannot leave that direct child running. Do not silently migrate old graphs
to `agentplane.diff` or reinterpret prior trace facts.

## Alternatives considered

### Ship a Unix-like diff executable on Windows

This would add a binary and descriptor-translation dependency, with more
packaging and executable-trust surface. It would still leave the preopen race.

### Reuse the old command string for a new implementation

This would silently change existing graph behavior and evidence. A new fixed
value makes the semantic difference explicit and preserves round trips.

### Permit arbitrary command paths

This turns a deterministic Gate into graph-controlled process execution and
violates the read-only trust boundary.

## Consequences

### Positive

- One portable comparison can be validated and executed without a shell or
  system `diff` installation.
- Old graph files and accepted results preserve their current semantics.
- No file contents are emitted as evidence by the new implementation.

### Negative / trade-offs

- Per-platform safe-open code and native race tests are required.
- A bytewise verdict does not show textual diff hunks; inspector content must
  use separate, explicitly authorized read surfaces.
- The old command remains unavailable on Windows until a graph author opts in
  to the new fixed value.

## Compatibility and migration

No graph schema version, edge, output, or event schema changes. The allowed
`command` enum gains one exact value. Parse/serialize round trips preserve both
strings. Existing Darwin/Linux graphs keep `/usr/bin/diff`; an author may change
a graph to `agentplane.diff` deliberately. No automatic rewriting occurs.

## Validation

- Graph validation rejects other commands and irrelevant fields in stable order.
- CLI and desktop round-trip both old and new YAML values without semantic
  change. The old value executes on Darwin/Linux and fails explicitly on
  Windows.
- Native tests cover equal/different/empty/large inputs, missing files,
  permissions, symlink/reparse and path escape (including `cwd` ancestors),
  observed root replacement, pinned-handle behavior under a subtree rename,
  detectable mutation, cancellation,
  and the empty-stream evidence shape. Tests do not claim to detect an
  undetectable in-place rewrite.
- A forced sidecar-death test proves the legacy diff child is terminated by
  the owned process lifecycle.
