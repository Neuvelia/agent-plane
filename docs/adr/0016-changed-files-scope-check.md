# ADR-0016 - Git-observed changed-files scope checking

**Status:** accepted for F1.8 implementation
**Date:** 2026-10-20

## Context

Agent `allowed_paths` was advisory. The Codex adapter already observed index and working-tree changes, but provider output could not enforce a scope, and correction attempts needed a baseline that survived retries. The existing CheckResult has only green/red verdicts and `scope.checked_inputs` denotes ports, not allowed paths.

## Decision

Add opt-in embedded Gate kind `changed_files_scope`. A runtime-owned baseline is captured immediately before the first Agent attempt and retained for the complete correction cycle. It consists of HEAD identity for diagnostics, index entries, and working-file existence/mode/content hashes for tracked, untracked and ignored files. The current state is compared with that snapshot, not merely with HEAD. This excludes unchanged pre-existing dirt and catches further edits to dirty files, including files beneath existing ignore rules. A change present after an earlier rejected attempt remains visible until corrected. Git paths are sorted; a rename is evaluated as old-path removal and new-path addition, so both sides must be allowed. A checked result can be produced by a provider-neutral Go function without using adapter `changed_files`.

The Gate's workspace and allowed paths must match its Agent's declaration. Accepted rules are exact files, trailing-slash directory subtrees, terminal `/**` directory aliases and `./` for the whole repository. Other glob syntax, traversal, absolute paths and backslashes are rejected. Directory matching uses path-component boundaries.

The CheckResult retains version 1 and green/red verdicts. Add an optional typed `changed_files_scope` detail with mode, normalized allowed paths, scope-redaction flag, baseline HEAD and observed count. Git-sourced `evidence[].path` contains sorted out-of-scope paths, bounded by the existing evidence limit with an omitted count. Schema 8 distinguishes this additive durable result; schemas 1-7 remain readable. Green with reason `changed_files_outside_scope` represents a warning violation. Red with the same reason blocks or enters the existing correction loop. In warn mode, failed Git inspection gives a green, explicitly indeterminate diagnostic. In deny mode, failed inspection is an operational error, never a green verdict. Existing Return Envelopes derive verdict/reason/evidence without a second result model, and retain the original intended scope.

## Alternatives considered

Using `git diff --name-only` alone omits untracked files and cannot separate pre-existing dirty state from this unit. Taking a new baseline for each retry would hide persistent violations. Trusting adapter-reported files would let a provider omit a violation. Introducing a warning verdict would alter the established result and retry model. Standalone graph Gates lack a trusted pre-execution snapshot; the provider-neutral Go check API remains available without inventing a graph baseline token.

## Consequences

The check detects repository changes after execution; it does not prevent writes, roll them back, or establish filesystem isolation. Concurrent Shell or external writers may be attributed to the Agent. Scope baseline capture hashes ignored files too; a large ignored tree can exceed Git listing limits or make inspection fail, which remains an operational failure in deny mode and an observable indeterminate result in warn mode. Adapter changed-file metadata retains its existing nonignored behavior. Evidence is bounded, so an omitted count may represent additional paths; paths are retained according to the total payload budget rather than a separate 512-byte evidence cutoff. There is no new runtime dependency or remote service.

## Compatibility and migration

Existing graphs are unchanged unless they select the new Gate kind. Codex keeps its observed-file output by using the shared Git snapshot helper. The graph schema and CLI envelope versions remain unchanged. Schema 8 readers accept older events; older readers may reject schema 8 as expected for a new event contract.

## Validation

Temporary Git repositories exercise tracked/staged/untracked/ignored/deleted changes, rename boundaries, pre-existing dirt, warn/deny, Git failures, long-path evidence, sorted evidence, CheckResult serialization and correction across attempts. Required checks include `go test -race ./...` and `make check`.
