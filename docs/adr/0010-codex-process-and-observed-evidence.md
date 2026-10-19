# ADR-0010 - Codex process ownership and observed evidence

**Status:** accepted; implemented in F1.2
**Date:** 2026-10-19

## Context

F1.1 supplies a provider-neutral Agent boundary but no real process, explicit
CLI registration or Git-derived trace evidence. F1.2 must prove the installed
Codex CLI without introducing F1.3 streaming or F1.8 scope enforcement.

## Decision

### Explicit invocation and environment

The initial supported protocol is verified against installed Codex CLI
0.163.0-alpha.10.2. Detect the version and required global/exec flags with
probes bounded to 10 seconds each (plus bounded owned-group shutdown).
Invoke structured argv:

```text
codex --no-daemon --ask-for-approval never exec --sandbox workspace-write
  --ignore-user-config --ephemeral --json --color never --cd WORKSPACE
  --output-schema SCHEMA_FILE --output-last-message RESULT_FILE -
```

Task and advisory scope enter stdin. Never widen the sandbox, add writable
roots or bypass restrictions. CLI users opt in with `--adapter codex` and
`--codex-bin /absolute/path`. Authenticated model execution is explicit.
`environment_refs` names the real adapter's environment allowlist: resolve
only those names, require present values and reject invalid names/NUL. No
ambient HOME/PATH/CODEX_HOME or credentials are copied. Authentication remains
owned by Codex through explicitly supplied references; ignore personal config
but retain existing execution rules. Git evidence uses its own controlled
environment, independent of agent-supplied GIT_* values.

### Result and progress

Request strict final JSON `{summary, artifacts: [{ref, name?}]}` through a
schema and private last-message file. Bound each JSONL record and final result
to 1 MiB; cap emitted static statuses to 64 KiB per invocation. Parse privately,
ignore unknown well-formed progress, and emit only allowlisted static statuses
through the existing log seam. Never expose raw stderr/JSONL, reasoning,
provider error messages or tool arguments. Required malformed/oversized result
fails safely; usage/raw metadata remain absent unless explicitly supported.

### Git snapshot baseline

Require canonical workspace equal to the root of an initialized non-bare Git
worktree with HEAD. Reject unresolved index stages, submodules and unsupported
filesystem entries. Compare before/after index entries (blob/mode/stage) and
working-file existence/mode/content digests for tracked and nonignored
untracked paths. Hash symlink text without following it; validate path/ancestor
confinement, use NUL-delimited Git output and reject invalid UTF-8 paths.
Git inspection disables optional locks and filesystem monitors; do not invoke
configured hooks or filters for content hashing.

Return sorted unique changed records with observed provenance when either
representation changes. Unchanged dirty/staged/untracked files are omitted;
further changes, creation/deletion/rename and index transitions are included.
Completely reverted edits are omitted. Baseline tracked/nonignored paths stay
in the comparison set even if they become ignored: unchanged state is omitted,
while real modifications/deletions remain evidence. Initially ignored contents
are not hashed. Before execution, record Git's ignored file/directory names
using a bounded `ls-files --others --ignored --exclude-standard --directory -z`
listing. If a newly visible after-path matches an initially ignored file or
falls beneath an initially ignored directory, fail closed with `codex_git`:
its pre-run content baseline is unknown. This also conservatively rejects new
files beneath such a directory after ignore removal. Establish the desired
ignore rules before running, then rerun to obtain a known baseline. Unknown
initially ignored paths never become accepted outputs or success evidence.
Model claims cannot add paths. This records changes observed during the invocation, not authorship. An otherwise
idle workspace is required; initially ignored/outside contents are outside
this evidence contract, with the fail-closed visibility transition rule above.
Advisory scope is not enforced.

### Process ownership

Initially support Darwin/Linux; fail closed elsewhere. Start Codex in a fresh
process group with no shared daemon. On cancellation signal the group TERM,
allow 500 ms, then KILL the group even if its leader exited; bound reap/pipe
drain to another second. Preserve the context cancellation cause and report
shutdown failures. Clean up remaining owned descendants on normal exit too.
Process-group ownership covers ordinary descendants; deliberately escaped
setsid/setpgid descendants are outside this contract. This is not OS process
containment or filesystem isolation, which requires later work.

### Observation and events

Add an optional generic observed-executor interface combining scheduler-owned
correlation, log observation and typed evidence. Give it precedence, preserving
legacy streaming/correlated/plain interfaces. An optional adapter progress
interface leaves AgentAdapter unchanged. Scheduler never switches on provider.

Add typed `node_evidence` payload containing adapter_id, adapter_version and
changed_files records. Emit successful observed evidence before node_finished,
including known-empty arrays, independently of declared graph output ports.
Evidence does not accept outputs. Exclude task, summary, artifacts, environment
and raw metadata. Distinct fixed error categories/codes describe failure;
failed invocations publish no accepted graph outputs. Existing terminal
`diagnostic_codes` carries a finite safe executor code, never raw error text.
Configured log redactions also apply to evidence identity/version/path strings
before storage.

Emit event schema 2 per ADR-0006; decode both v1 and v2 existing event shapes,
and reject new event types under v1. CLI envelope stays v1 because events carry
their own schema. Snapshot/copy logic must detach evidence slices.

## Alternatives considered

- Shared daemon: rejected because child PID cancellation would not own work.
- Raw JSON/stderr forwarding: rejected because reasoning/secrets may leak.
- Subtracting status path sets: rejected because already-dirty files may change.
- Requiring clean workspaces: simpler but unnecessarily excludes documented
  dirty-baseline behavior; snapshot evidence is explicit and testable.
- Mandatory sandbox/container framework: outside F1.2 and adds dependencies;
  retain Codex sandbox plus the stated process-group limitation.

## Consequences

Local opt-in execution can use installed Codex auth without parent-environment
forwarding. Git evidence is stronger than model claims but is neither scope
enforcement nor attribution/isolation. Static progress intentionally precedes
F1.3. No Go dependency is needed. Unsupported platforms/protocols fail closed.

## Compatibility and migration

F1.1 interfaces, schema-v0 graph fields and Shell execution remain valid.
Only the real adapter resolves environment_refs; mock references remain inert.
Consumers of new events must support event schema 2; old v1 records remain
readable. No persistence migration or CLI envelope change is introduced.

## Validation

Fake-CLI tests cover detection/argv/environment, Git baselines, safe progress,
results, failures and process-group cleanup. Runtime/CLI integration proves
correlation and Shell compatibility; schema tests prove version compatibility.
A mandatory installed-CLI disposable-repository smoke records version,
graph/run/node IDs, actual edit and node_evidence. Full make check and race
suite must pass. Fake-only evidence cannot complete F1.2.
