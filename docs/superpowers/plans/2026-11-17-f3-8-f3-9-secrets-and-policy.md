# F3.8 Secrets References and F3.9 Execution Policy v0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a graph name secrets by reference only and state per-node
permissions; resolve and deliver each secret only to the nodes assigned it,
redact every resolved value everywhere the runtime writes, show one consent
report before the run, and (in enforce mode) prevent what the platform can
prevent and refuse to start when it cannot.

**Architecture:** Pure domain code (`internal/domain/secrets`,
`internal/domain/policy`, additive graph fields) owns grammar, narrowing,
the shared path matcher and the enforcement decision table. `internal/runtime`
owns one preflight pipeline and two consumer-owned ports, `SecretResolver` and
`PolicyEnforcer`, like `WorkspaceManager` (ADR-0024). Concrete providers and
platform mechanisms live in new adapter packages that only `cmd/agentplane`
imports; an architecture-test rule proves it. The Linux Landlock helper is the
runner binary re-executed.

**Tech Stack:** Go standard library only (`syscall`, `os/exec`,
`syscall.NewLazyDLL` on Windows), no new dependency, no `go.mod` change.

**Specs:** `docs/features/F3.8-secrets-references.md`,
`docs/features/F3.9-execution-policy-v0.md`.
**ADR:** `docs/adr/00XX-secret-references-and-execution-policy.md` (proposed,
number assigned at merge).
**Evidence:** spike report `experiments/spikes/2026-11-17-landlock-namespaces-jobobject.md`
(branch `claude/spikes-landlock-jobobject`, not merged); it is the only
platform evidence this plan relies on. Tarmind (in code today: agentplane)
keeps `agentplane` identifiers, flags and file formats; only new prose says
Tarmind.

## Global Constraints

- Implement F3.8 and F3.9 as specified. No keychain provider (F3.8b), no
  Windows AppContainer or restricted tokens (F3.9b), no read confinement, no
  host or port network rules, no loopback route or desktop view for the
  report, no policy language, no macOS Shell enforcement.
- A graph without `secrets` or `policy` keeps byte-identical semantic hash,
  events, CLI output, JSON envelope and scheduling. Existing tests pass
  unmodified; a golden test pins this once (T2) and is re-run by every later
  task.
- Resolved values live only in process memory, in one opaque type that cannot
  be formatted or serialized (T3). Never in `Options` fields that are
  serialized, events, the store, API bodies, error strings, panics or logs.
- Every static failure is a `run.PreflightError` with the existing diagnostics
  shape and emits no events. Resolution happens after policy evaluation and
  before `run_started`.
- Enforce mode fails closed and never runs a node less confined than reported.
  Audit mode blocks nothing and the helper, namespaces and launcher checks are
  not applied in audit.
- No new Go dependency. Platform code carries build tags with an `other`
  implementation that reports `unenforceable`.
- `internal/runtime` imports neither the providers nor the platform package.
- No sleeps as synchronization; helper-process and namespace tests wait on the
  child. `go test -race` on every touched package.
- One branch and one PR per task, in order, each stacked as stated. Each task
  ends with `make check`, `go test -race` for touched packages and a review of
  `git diff --stat`. Never merge without an explicit request.
- Event schema number is never a literal in this plan: "next free at merge"
  (T1 states the procedure).

## Review Focus

- A secret cannot reach a node the policy does not admit, through any channel
  (`env`, `env_allowlist`, `input_env`, `environment_refs`, `args`, `task`).
- The redaction set: empty values, encodings, all three Base64 alignments,
  marker safety, streaming tail, and cost bounds.
- The byte-scan oracle is independent of the code it tests (T5).
- The Landlock helper can only narrow authority and fails closed before exec.
- Preflight probes are real (clone plus connect), bounded and cached, never
  sysctl reads.
- Audit versus enforce: `applied` is recorded, so audit never reads as
  protection.
- Windows launcher resolves once, checks that path and launches that path.

## In-flight branches and rebase points

All paths below were read from the branches on 2026-11-17. Base for this plan
is `main` at `e55af36` (event schema 11, no F3.1 runtime yet).

| Branch (state) | Touches that overlap this plan | What we do |
| --- | --- | --- |
| F3.1 stack: `claude/f3-1-runtime` -> `claude/f3-1-cli` -> `claude/f3-1-guard-and-docs` (implemented, PRs open) | `internal/runtime/{preflight,scheduler,batch,accept,workspace,isolation}.go`, `internal/agent/{config,fields}.go` (`isolation`), `cmd/agentplane/cli.go` flags and usage string, `run.Result.Isolation`, `isolatedNode()` deep copy, `prepareIsolated` (emits `workspace_prepared`) | T1, T2, T4a, T5 do not touch these files and start on `main`. **T3 starts after `claude/f3-1-runtime` merges** (it adds `preflightIsolation` in `preflightRun` between `preflightLoops` and `preflightHuman`; our pipeline goes immediately after it, in a new file with one call line). **T10 starts after `claude/f3-1-cli` merges** (usage string, flag block). **T12 (Agent states) starts after F3.1 runtime merges**: it needs `isolation` accepted by the Agent config and the effective-worktree seam. |
| F3.4 plan (`claude/f3-4-plan`; spec only, no code) | Will change `internal/shell/executor.go` (process ownership, `Cancel`, Job Object, `SysProcAttr`), `internal/agent/codex` process files, adds `node_finished` reason, `timeout_seconds`, one event schema bump | Launcher (T9) is `Prepare(cmd)` over an already built `exec.Cmd`, mutating `SysProcAttr` field by field (we set `Cloneflags`, `UidMappings`; F3.4 sets `Setpgid` or Job attributes). If F3.4 lands first, rebase T9-T11 onto its process helper and call `Prepare` before the helper starts the command. Whoever merges second takes the next event schema number. |
| F3.2 plan (`claude/f3-2-plan`; spec only) | `internal/store` summary, lock and snapshot files; its tests read fixtures for schemas 1-11 and a "higher schema is unsupported" fixture | Our store scan helper walks the whole store directory recursively, so F3.2's new files are covered with no change. F3.2's "newer schema" fixture must derive its number from `events.SchemaVersion + 1`, not a literal: tell its owner when T1 merges. Resume (F3.3) must re-run this pipeline and never read secrets from the store (F3.8 requirement 7). |
| F2.8b (reserves event schema 12) | `internal/domain/events` | Re-read `events.SchemaVersion` on `origin/main` at the start of T1 and again just before merge. |
| Spike branch `claude/spikes-landlock-jobobject` | build-tagged code under `experiments/spikes` only | Read for syscall numbers and flag values; nothing imports it. Product code is written fresh under `internal/adapter/sandbox`. |

## Task order and dependencies

```text
T1 events (schema, payloads, kinds) ─────────────┐
T2 domain secrets + graph schema + fields ──┐    │
                                            ├─> T3 runtime pipeline (needs F3.1 runtime on main)
T4a redaction set + encodings ──────────────┘    │      │
T4b delivery to Shell/Agent ─────────────────────┘      ├─> T5 provider + CLI/serve + scan + e2e  (F3.8 done)
                                                         │
                          T6 domain policy + shared matcher ──> T7 policy schema + validation
                                                         │              │
                                                         └──────> T8 runtime policy pipeline + audit
                                                                        │
                                              T9 report + CLI (needs F3.1 CLI)
                                                                        │
                       T10 launcher: shell/commands + policy_blocked ───┤
                       T11 Linux Landlock + netns + real probe ─────────┤  (enforce)
                       T12 Agent states + Codex pinning (needs F3.1 runtime)
                                                                        │
                                           T13 CI job, docs closure, ADR number
```

T1, T2, T4a and T6 are independent of each other and can run in parallel.
F3.8 (T1-T5) is complete and shippable before F3.9 starts: the policy seam it
needs is a no-op admit function that T8 fills in.

## Spec ambiguities and decisions to confirm before T3

1. **Mid-glob patterns.** F3.9 shows `src/**/*.go`, but the F1.8 grammar this
   spec says to reuse (`normalizeScopeRules` in `internal/gate/changed_files.go`)
   accepts only exact paths and a trailing `/**`; any other `*?[]` is rejected.
   Plan: policy v0 accepts exactly the F1.8 grammar, so Landlock granularity
   equals pattern granularity and the "file-level part stays detective" sentence
   is dropped from the docs. Amend the spec in T6.
2. **`commands` semantics.** Plan: a name entry matches when the node's
   `command` string equals it and resolves; an absolute entry matches the
   resolved absolute path after `EvalSymlinks` (case-insensitive on Windows).
   Both launch the resolved absolute path. Report prints which `go` was
   resolved. Recommend absolute entries for strictness; confirm.
3. **`applied` flag.** The spec records per-permission "state" in audit. In
   audit nothing is applied, so `prevented` would mislead. Plan: each
   permission carries `state` (what this machine can do) and `applied`
   (true only in enforce and only for prevented, contained, delegated). Report
   and docs say "would be prevented" in audit. Spec amendment in T1.
4. **Shell `paths.write` without Landlock.** The platform matrix says "F1.8
   detects when Git evidence works", but F1.8 baselines exist only for Agent
   nodes. Plan: no Shell baselines in v0; Shell `paths.write` without Landlock
   is `unenforceable` (enforce fails preflight) and `unobserved` in audit.
5. **Where `policy_evaluated` is emitted for isolated Agents.** The F3.1
   worktree is created at dispatch, after preflight, so the effective root is
   unknown at preflight. Plan: preflight decides states against the root
   *kind* (`worktree`, `workspace`, `cwd`); the coordinator emits
   `policy_evaluated` at dispatch immediately after `workspace_prepared` and
   before the first `attempt_started`. Non-isolated nodes emit at the same
   point with the preflight-resolved root.
6. **Blocked node and Check Result.** A launch refusal has no Gate. Plan: emit
   the red `execution_policy` Check Result through the existing `gate_check`
   fact with the node's attempt correlation; verify in `cycle()` (loop.go) that
   an attempt exists for a refused non-loop node, otherwise a minimal attempt is
   recorded first. Decide while reading `cycle()`; do not invent an envelope.
7. **`serve` and secrets.** The spec recommends `serve` resolves from its own
   environment. A hostile local writer of graphs (ADR-0021 write trust) could
   then assign `env/ANYTHING` to a Shell node. Plan follows the spec and ships
   `TestServeNeverReturnsSecretValues`; the permissions report for `serve` and
   an explicit opt-in flag are a recorded follow-up, not built here. Confirm.
8. **Redaction cost bound.** Plan adds a hard bound: at most 64 resolved
   secrets, each at most 16 KiB, at most 16 forms per secret; beyond it
   `secret_invalid_value`. The redactor scans every position against every
   form, so unbounded values would stall log delivery. Confirm numbers.
9. **Collision vocabulary.** "Collision" is used for three distinct mode
   independent errors, each its own code: `E_SECRET_ENV_COLLISION` (validation,
   `secret_env` name vs `env`, `env_allowlist`, `input_env`,
   `environment_refs`, case-insensitive), `secret_ambient_bypass` (preflight,
   backing variable reachable by another node), and the existing "no safe
   replacement marker" redactor error, now also raised for resolved secrets.

## Spec criteria to tests

| Criterion | Test (task) |
| --- | --- |
| F3.8 secrets entry with extra field fails naming it | `TestValidateSecretsRejectsValueAndExtraFields` (T2) |
| F3.8 `secret_env` undeclared, bad env name, Gate/Human | `TestValidateSecretEnv*` (T2) |
| F3.8 unset env var fails `secret_unresolved`, no run dir | `TestPreflightSecretUnresolvedNoEventsNoRunDir` (T3, T5) |
| F3.8 empty secret `secret_empty` | `TestEnvProviderEmpty`, `TestPreflightSecretEmpty` (T3, T5) |
| F3.8 per-node grant isolation (names and sizes) | `TestGrantReachesOnlyAssignedNode` (T4b, T3) |
| F3.8 no secret byte in any store or output file | `TestRunStoreAndOutputsHoldNoSecretBytes` (T5) |
| F3.8 split across two writes | `TestResolvedSecretSplitAcrossWrites` (T4a) |
| F3.8 literal embedded in graph rejected before store copy | `TestGraphGuardRejectsResolvedValue` (T5) |
| F3.8 `secret_ambient_bypass` audit and enforce | `TestPreflightSecretAmbientBypassAudit`, `TestPreflightSecretAmbientBypassEnforce` (T3 audit, T8 enforce) |
| F3.8 serve never returns a secret | `TestServeNeverReturnsSecretValues` (T5) |
| F3.8 encodings and three Base64 alignments | `TestRedactionBase64AllAlignments`, `TestFormsMatchIndependentOracle` (T4a) |
| F3.8 byte-identical without `secrets` | `TestGraphWithoutSecretsOrPolicyIsByteIdentical` (T2, re-run after T3, T8) |
| F3.8 `secret_in_workspace` red result | `TestSecretInWorkspaceRedCheckNamesPathNotValue` (T5b) |
| F3.8 architecture: providers imported by root only | `TestProvidersAndPlatformImportedOnlyByRoot` (T5) |
| F3.9 widening default fails validation | `TestValidatePolicyNodeWidensDefault` (T7) |
| F3.9 graph cannot lower runner mode | `TestEffectiveModeIsStrongerOfRunnerAndGraph` (T6, T9) |
| F3.9 `graph permissions` closed vocabulary, stable JSON, secret-free | `TestGraphPermissionsJSONStable`, `TestReportHoldsNoSecretValue` (T9) |
| F3.9 Landlock write confinement on the filesystem | `TestLandlockShellWritesOnlyAllowed` (T11) |
| F3.9 Landlock unavailable -> `policy_unenforceable` | `TestEnforceLandlockUnavailableFailsPreflight` (T8 with fake probe) |
| F3.9 `network: off` loopback refused, `on` allowed | `TestNetnsOffRefusesLoopback`, `TestNetOnConnects` (T11) |
| F3.9 `shell: off` / `commands` never start, `policy_blocked` | `TestLauncherNeverStartsRefusedCommand` (T10) |
| F3.9 resolved path equals launched path | `TestLauncherChecksAndLaunchesSameAbsolutePath` (T10) |
| F3.9 Agent `paths.write` needs isolation | `TestEnforceAgentPathsWriteRequiresWorktree` (T12) |
| F3.9 isolated Agent out-of-scope is not integrated | `TestIsolatedAgentOutOfScopeNotIntegrated` (T12) |
| F3.9 blocked node not retried, siblings intact | `TestPolicyBlockedNodeIsNotRetried`, `TestPolicyBlockedLeavesSiblingsAccepted` (T10) |
| F3.9 audit records, never changes outcome | `TestAuditRecordsViolationsAndChangesNothing` (T8) |
| F3.9 undeclared admission denies secret | `TestSecretPolicyDeniedNeverInEnvironment` (T8) |
| F3.9 secret-free byte scan | shared with T5 scan helper, re-run in T8, T10 |
| F3.9 Windows launcher; `paths`/`network` unenforceable, audit unobserved | `TestWindowsLauncher*`, `TestWindowsDecisionTable` (T10) |
| F3.9 architecture: platform and providers root only | T5 test, extended in T10 |
| F3.9 `-race`, no leftover child | `-race` runs in each task; helper tests wait on the process (T11) |

---

### Task 1: Events, check kinds and the schema bump

**Branch:** `claude/f3-8-events` (stacked on `claude/f3-8-f3-9-spec`)

**Files:** Create `internal/domain/events/policy.go`, `policy_test.go`. Edit
`internal/domain/events/event.go` (schema constant, `Validate`, clone and wire
decode switch like `workspace.go`), `internal/domain/events/gate.go` (two new
`CheckKind` values), `internal/domain/events/AGENTS.md`,
`docs/architecture.md` (event list), `docs/features/F3.9-*.md` (the `applied`
amendment, decision 3), `desktop/src/reducer.ts` test (unknown types tolerated).

**Schema number (procedure, not a literal):**

- [ ] Read `events.SchemaVersion` on `origin/main`, the F2.8b reservation (12)
  and the F3.4 proposal. Take the next free number. Name it
  `SchemaPolicy` in the `Validate` gate so a later rebase changes one line.
  Record the final number in the ADR in the same PR.

**Contract:**

| Type | Payload (bounded, slices detached on clone) |
| --- | --- |
| `policy_evaluated` | `mode`, `applied` (bool), `scope_root_kind`, `scope_root` (absolute, empty when none), `permissions` (closed names `paths_write`, `network`, `shell`, `commands`; each `{requested, value, state, applied}`), `secrets` (<= 64 of `{name, provider, env, resolved}`), `redaction_values` (count only) |
| `policy_violation` | `permission`, `reason` (`launch_refused`, `out_of_scope_write`, `secret_in_workspace`, `enforcement_unavailable`), `outcome` (`blocked`/`recorded`), `evidence` (<= 64 sorted, each <= 512 bytes), `omitted` |

New `CheckKind`: `execution_policy`, `secret_in_workspace`; they reuse
`GateEvidence` entries and add no details struct.

- [ ] Red first: `TestPolicyEventsRequireNewSchema`,
  `TestPolicyEventsRoundTripAndBounds`, `TestPolicyEventsRejectValueLikeFields`
  (a validator that refuses any field outside the closed vocabulary, so a value,
  length or hash cannot be added later by accident), `TestPolicyEventsCloneIsDetached`,
  `TestOlderSchemaFixturesStillDecode` (schemas 1-11), store JSONL round trip.
- [ ] Correlation: `policy_evaluated` requires run and node run;
  `policy_violation` additionally accepts an attempt.
- [ ] Desktop reducer test with a `policy_evaluated` event proves unknown types
  are ignored (same pattern as F3.1 T2).

---

### Task 2: Domain secrets and the additive graph schema

**Branch:** `claude/f3-8-graph` (on `claude/f3-8-events` or `main`)

**Files:** Create `internal/domain/secrets/{doc,ref,name}.go` and tests,
`internal/domain/graph/secrets.go`. Edit `internal/domain/graph/definition.go`
(`Secrets map[string]SecretDefinition`), `internal/domain/graph/version.go`
(nothing if the field is zero when absent; add the golden test),
`internal/domain/graph/validation.go` (call `validateSecrets` after
`validateUnits`), `internal/adapter/yamlgraph/dto.go` (`Secrets` with
`yaml:"secrets,omitempty"`, both mappings), `internal/shell/fields.go` and
`internal/agent/fields.go` (`secret_env`), `internal/shell/executor.go`
`parseConfig` and `internal/agent/config.go` `parseNode` (read and validate
`secret_env`; keys derive from the descriptors),
`internal/fields/fields.go` (`FormatSecretName`, `FormatEnvName` reuse),
`internal/fields/fieldstest/fieldstest.go`,
`internal/adapter/yamlgraph/edit` (preserve unknown top-level keys),
`docs/graph-spec.md`.

- Domain `secrets`: `ParseRef(string) (Ref, error)` (provider
  `[a-z][a-z0-9-]{0,31}`, name 1-128 printable ASCII, no `/`, no whitespace);
  `ValidName` (`[a-z][a-z0-9_]{0,63}`); known providers `env`; `local` reserved
  and reported "provider not available" (F3.8 requirement 2).
- Graph validation (all in `graph.Validate`, so `graph validate`, `run` and the
  editor API share it through `validateGraph` in `cmd/agentplane/validation.go`):
  entry with any field other than `secret_ref`; invalid name or reference;
  unknown or reserved provider; `secret_env` naming an undeclared secret;
  invalid or duplicate env name; case-insensitive collision with `env`,
  `env_allowlist`, `input_env`, `environment_refs` (`E_SECRET_ENV_COLLISION`);
  `secret_env` on a Gate, Human or loop-embedded Gate; graph `defaults` cannot
  carry secrets (there is no `defaults` field yet, so assert "no such key"
  rather than build one; T7 adds `policy.defaults` and asserts it there).
- Agent `secret_env` is gated by spike S1 (T4b). Until S1 reports, `agent`
  descriptors do not list `secret_env` and the validator names the node and
  says "secret_env is not supported on agent nodes yet".

**Tests (red first):**
`TestParseRefGrammar`, `TestValidateSecretsRejectsValueAndExtraFields`,
`TestValidateSecretEnvUndeclaredInvalidNameAndNodeTypes`,
`TestValidateSecretEnvCollisionsCaseInsensitive`,
`TestSecretsRoundTripKeepsFieldOrderAndComments` (yamlgraph edit),
`TestGraphWithoutSecretsOrPolicyIsByteIdentical` (semantic hash fixture from
`main`, plus `Marshal` bytes), `fieldstest` coverage of the new descriptors
(every key the validator accepts has a descriptor and the reverse).

---

### Task 3: Runtime pipeline, resolver port and grants

**Branch:** `claude/f3-8-runtime` (after `claude/f3-1-runtime` merges)

**Files:** Create `internal/runtime/secrets.go`, `internal/runtime/pipeline.go`,
tests. Edit `internal/runtime/scheduler.go` (`Options.Secrets SecretResolver`,
`ExecutionObservation.Secrets SecretGrant`), `internal/runtime/preflight.go`
(one call line after `preflightIsolation`; `runPreparation` gains `grants`),
`internal/runtime/batch.go` (pass the node's grant into the observation),
`internal/runtime/AGENTS.md`, `docs/architecture.md`.

**Interfaces (consumer-owned, indicative):**

```go
type SecretRequest struct{ Name string; Ref secrets.Ref }       // from the graph
type SecretResolver interface {
    Resolve(ctx context.Context, reqs []SecretRequest) (ResolvedSecrets, error)
}
type ResolvedSecrets interface {               // opaque; no exported values
    Values() []string                          // only for the redaction set
    Grant(nodeID string) SecretGrant
}
type SecretGrant struct{ /* unexported env pairs */ }
func (g SecretGrant) Environ() []string         // the one delivery path
func (SecretGrant) String() string               // "[secret grant]"
func (SecretGrant) GoString() string; Format(fmt.State, rune)
func (SecretGrant) MarshalJSON() ([]byte, error) // always an error
func (SecretGrant) MarshalText() ([]byte, error) // always an error
```

The pipeline function (`runtime.preparePolicyAndSecrets`) fixes the order the
ADR requires: validate (done earlier) -> evaluate policy -> probe -> decide ->
deliver-or-deny -> resolve admitted -> build redaction set. In this task the
policy and probe steps are one function value `admit func(node, secret) error`
defaulting to allow, and the order test already exists with fakes so T8 only
replaces the default.

- [ ] Resolve only secrets assigned to at least one node; a declared but
  unassigned secret is never resolved (`TestUnassignedSecretNotResolved`).
- [ ] Preflight errors (`PreflightError` diagnostics, codes from the spec:
  `secret_unresolved`, `secret_empty`, `secret_provider_unavailable`,
  `secret_invalid_value`, `secret_policy_denied`, `secret_ambient_bypass`),
  each naming secret, node and a correction, never the value or the variable
  contents. Provider errors are wrapped without values.
- [ ] `secret_invalid_value`: NUL, over the bound (decision 8), and not valid
  UTF-8 when `executor.(ObservedExecutor)` (reuse the existing observed rule in
  `preflightRun`).
- [ ] Ambient-bypass detector (pure, `internal/domain/secrets/bypass.go`):
  for every `env`-provider reference, any node not assigned it that names the
  backing variable in `env_allowlist`, `environment_refs`, an `env` key or an
  `input_env` target fails in every mode. Case-insensitive compare on every OS.
- [ ] The redaction set passed to `newLogRedactor` is `LogRedactions` plus
  resolved values plus their forms (T4a). `redaction_values` count is exposed
  for T1's payload. `--redact-env` semantics unchanged (empty skipped).
- [ ] `ExecutionObservation` is never serialized; add a reflection test that
  walks every runtime struct reachable from events and `run.Result` and fails
  on a `SecretGrant` field.

**Tests (red first):**
`TestPreflightSecretUnresolvedNoEventsNoRunDir`, `TestPreflightSecretEmpty`,
`TestPreflightSecretAmbientBypassAudit`, `TestPreflightSecretInvalidValue`,
`TestPipelineOrderPolicyBeforeResolveBeforeRunStarted` (recording fakes),
`TestGrantReachesOnlyAssignedNode`, `TestSecretGrantCannotBeFormattedOrMarshaled`
(`%v`, `%+v`, `%#v`, `%s`, JSON, text, `fmt.Errorf("%w")` wrapping),
`TestResolveCancelledByContext`, `TestNoSecretTypeReachableFromEventsOrResult`,
absent-`secrets` identity tests (events and result unchanged).

---

### Task 4a: Redaction set expansion

**Branch:** `claude/f3-8-redaction` (independent; on `main`)

**Files:** Create `internal/domain/secrets/forms.go`, `forms_test.go`. Edit
`internal/runtime/log_redactor.go` (dedupe, bounds), `log_redactor_test.go`,
`internal/runtime/progress.go` and `values.go` only if the larger `maxLen`
needs it (it should not: they read `maxLen` from the redactor).

`secrets.Forms(value string) [][]byte` returns, for a value of at least 8
bytes: raw; lower and upper hex; `url.QueryEscape` and `url.PathEscape` forms
(only when different from raw); standard and URL-safe Base64 for each of three
alignments. A value of fewer than 8 bytes yields raw only (decision: allow,
warn in the preflight diagnostics, do not reject; F3.8 open question 3).

Base64 for alignment `k` in `{0,1,2}` (k unknown bytes precede the secret in
the surrounding stream) is the stable substring of `enc(k zero bytes || S)`
from character `ceil(8k/6)` to `floor(8(k+len(S))/6)` using the unpadded
alphabet, so characters that depend on neighbouring bytes and padding are
excluded. For `k=0` that is the leading `floor(8n/6)` characters; for `k=1`
skip two, for `k=2` skip three. Standard and URL alphabets are encoded
separately. MIME line wrapping and other encodings are a documented residual,
not covered.

- [ ] Redactor: forms are added to `secrets` ordered longest first (existing
  sort), deduplicated by bytes, bounded by decision 8. The marker is chosen
  from bytes absent from **every form** (the set now includes `+/-_=%`), still
  erroring with the existing "no safe replacement marker" when none exists, and
  that error is raised for resolved secrets in every mode.
- [ ] The redactor must stay byte-identical for `LogRedactions` only (existing
  `log_redactor_test.go` unchanged and green).

**Tests (red first):**
`TestFormsMatchIndependentOracle` (the oracle is a brute-force reimplementation
inside the test: for each `k`, embed the secret after `k` random prefix bytes
and before 0-5 random suffix bytes in many random buffers, base64 the whole
buffer with `StdEncoding`, `URLEncoding`, `RawStdEncoding`, `RawURLEncoding`,
and assert some form occurs in the output; no code shared with `Forms`),
`TestRedactionBase64AllAlignments` (feeds the encoded buffers through the real
redactor across chunk splits and asserts no residue of the secret),
`TestResolvedSecretSplitAcrossWrites`, `TestFormsShortValueRawOnly`,
`TestFormsHexAndPercentCaseVariants`, `TestRedactorMarkerAvoidsEncodedBytes`,
`TestRedactorBoundsRejectOversizedSet`, a benchmark guard
`BenchmarkRedactorWithForms` (not a gate; recorded in the PR to show the
cost of the larger set).

---

### Task 4b: Delivery to Shell and Agent nodes

**Branch:** `claude/f3-8-delivery` (on T3)

**Files:** Edit `internal/shell/executor.go` (`ExecuteNodeGranted(ctx, id, n,
in, observe, attempt, extraEnv []string)`; existing methods delegate with nil;
`execute` appends `extraEnv` last so a node cannot shadow a granted name),
`cmd/agentplane/adapters.go` (`nodeExecutor` passes `o.Secrets.Environ()`),
`internal/agent/contract.go` (`SecretEnv []string` tagged `json:"-"`),
`internal/agent/executor.go` (copy from observation), `internal/agent/codex/adapter.go`
(`environment()` appends `SecretEnv` after `EnvironmentRefs`),
`internal/agent/adaptertest`.

- [ ] **Spike S1 first (gate for Agent delivery).** Using the installed Codex
  CLI on a disposable repository, record in
  `docs/validation/F3.8-codex-env-spike.md`: does a variable named like a
  secret reach the commands Codex runs, is it filtered by Codex's shell
  environment policy, and can the value be kept out of the prompt. If it does
  not reach tool commands, Agent `secret_env` stays rejected at validation (T2)
  and F3.8 ships Shell-only; the spec question 6 is closed either way.
- [ ] Shell: the granted pairs appear in the child environment; nothing else
  changes. `AGENTPLANE_ATTEMPT` stays reserved.
- [ ] Agent contract tests (`TestAgentInputJSONNeverContainsSecretEnv`,
  `TestAdapterErrorsNeverContainSecretValues`): a failing adapter error is
  wrapped without environment values; `fail(...)` messages stay static.

**Tests:** `TestShellGrantedEnvVisibleToChildOnly` (child prints names and
count, never the value), `TestShellGrantCannotBeShadowedByEnv`,
`TestSiblingShellNodeDoesNotSeeGrant` (shared with T3), codex adapter test with
the fake binary that prints its environment names.

---

### Task 5: Provider, CLI and serve composition, store scan, detection

**Branch:** `claude/f3-8-cli` (on T3, T4a, T4b; `cli.go` flag edits wait for
`claude/f3-1-cli`)

**Files:** Create `internal/adapter/secrets/env/{provider}.go` and tests,
`internal/testenv/secretscan.go`, `cmd/agentplane/secrets.go` (composition:
provider selection and the guarded resolver). Edit `cmd/agentplane/cli.go`
(`runtime.Options.Secrets`, `--redact-env` help text states redaction is best
effort), `cmd/agentplane/serve.go` (same composition in `starter`),
`internal/archtest/archtest_test.go` (new rule), `internal/runtime/*` (T5b
detection), `docs/security-model.md`, `docs/graph-spec.md`,
`docs/codebase-map.md`.

- [ ] `env` provider: `os.LookupEnv(name)` read once per run through the
  resolver; classification into found, unset, empty. `local` returns
  `secret_provider_unavailable`. The package holds no state, runs no process.
- [ ] **Guarded resolver (decorator in `cmd/agentplane`).** It wraps the
  provider so that, inside the runtime preflight step, after values exist and
  before `run_started`, it checks the graph bytes, the parsed definition and the
  raw `--input` values against every resolved value and `--redact-env` value
  (generalizing `graphHoldsSecret`; `definitionHoldsSecret` is reused as is) and
  returns a typed preflight error. This keeps the byte copy of the graph in
  the composition root while the order is owned by the runtime. The existing
  early `graphHoldsSecret` call stays for `--redact-env` values so an existing
  failure mode keeps its message and timing.
- [ ] **Scan helper** `testenv.AssertNoSecretBytes(t, root, values...)`: walks
  the directory recursively, scans each file for raw and all `secrets.Forms`
  bytes, and for `events.jsonl` also decodes each line, base64-decodes every
  `[]byte` field (log and progress data) and scans the decoded text. A leak
  inside a Base64 log chunk at any alignment is therefore found even if the
  redactor failed to see it. Used for store directories, captured CLI output
  and API bodies. Its own tests inject a leak in each place and fail the scan.
- [ ] **Detection (T5b, same branch, separate commit).** After a node with a
  grant ends, scan the files it changed (the F1.8 Git-observed set, or the F3.1
  worktree diff) for the raw granted values: at most 256 files, 8 MiB per file,
  32 MiB total, symlinks not followed, regular files only. A hit adds a red
  Check Result `secret_in_workspace` naming path and secret name through the
  existing evidence path and a `policy_violation` with outcome `recorded`
  until T8 (node outcome by mode). Where Git evidence is unavailable the scan
  reports `unobserved`. The scan reads through `io.LimitedReader` and never
  logs content.
- [ ] Architecture test: add `TestProvidersAndPlatformImportedOnlyByRoot` to
  `internal/archtest`. Classify `internal/adapter/secrets/...` and
  `internal/adapter/sandbox/...` as adapter packages (the existing rule already
  forbids adapter-to-adapter imports) and add an explicit rule that only
  `cmd/agentplane` may import those two prefixes; no baseline entry.

**Tests (red first):**
`TestEnvProviderFoundUnsetEmptyNul`, `TestGraphGuardRejectsResolvedValue`
(`env`, `args`, `task`, YAML comment, escaped scalar), `TestRunStoreAndOutputsHoldNoSecretBytes`
(built-binary `agentplane run --store-dir` with a Shell node that echoes its
secret to stdout, stderr, a JSON output and, through a fake Codex, a progress
event; scan the whole store plus captured stdout and stderr),
`TestServeNeverReturnsSecretValues` (every response body, header and error of
a served run that echoed its secret), `TestRecordValuesRedactsResolvedSecret`,
`TestSecretInWorkspaceRedCheckNamesPathNotValue`,
`TestSecretScanBounds`, `TestProvidersAndPlatformImportedOnlyByRoot`.

**F3.8 is done here:** every F3.8 criterion has a named passing test; docs
state that `local` is reserved and that redaction is best effort.

---

### Task 6: Domain policy, shared matcher, decision table

**Branch:** `claude/f3-9-policy-domain` (independent of F3.8; on `main`)

**Files:** Create `internal/domain/policy/{doc,policy,narrow,mode,scope,decide,report}.go`
and tests. Edit `internal/gate/changed_files.go` (use the shared matcher),
`internal/domain/events/unit.go` (`NormalizeUnitScope`) is reused read only. Edit
`docs/features/F3.9-execution-policy-v0.md` (decision 1 amendment).

- [ ] **Characterize first.** Add tests in `internal/gate` that pin today's
  `normalizeScopeRules` and `scopeAllows` behavior (similar prefix
  `internal/runtime2/` vs `internal/runtime/**`, `./`, exact file, duplicates,
  untracked/deleted/renamed handled by the caller). They pass on `main`
  unchanged, then the functions move to `policy.NormalizeScope` and
  `policy.ScopeAllows`; the gate calls them. The diff of the gate tests is
  empty. No second matcher.
- [ ] Types: `Mode` (`audit`, `enforce`), `Permission` closed names,
  `Request` (all fields optional pointers so "not declared" is distinct from
  empty), `Effective`, `State` closed vocabulary (`prevented`, `contained`,
  `delegated`, `detected`, `unobserved`, `unenforceable`), `Capabilities`
  (plain struct filled by adapters: `LandlockABI int`, `UserNetNS bool`,
  `JobObject bool`, `GitEvidence bool`, `CodexSandbox` enum, `OS string`).
- [ ] `Narrow(defaults, node)`: a node value may only be stricter (paths subset
  by `ScopeAllows`, `on`->`off`, smaller command set); widening is a typed error
  naming the key. `StrongerMode(runner, graph)`.
- [ ] `Decide(eff Effective, root RootKind, nodeType, caps, mode)` pure,
  producing a state per permission exactly as the F3.9 platform matrix, plus
  `applied`. The decision table is a data-driven test over every cell of the
  matrix, including macOS and "probe failed" (treated unavailable, never
  assumed enforced).
- [ ] `Report` (Go type with JSON form) built from decisions; field order and
  JSON are stable; it has no field that could hold a secret value.

**Tests (red first):** `TestNarrowRules`, `TestEffectiveModeIsStrongerOfRunnerAndGraph`,
`TestScopeMatcherParityWithF18`, `TestScopePatternsRejectAbsoluteEscapingUncAds`
(`..`, drive letters, UNC, `\\?\`, `:stream`, NUL), `TestDecisionTableEveryCell`,
`TestDecideNeverAppliesInAudit`, `TestReportJSONStable`.

---

### Task 7: Policy in the graph schema

**Branch:** `claude/f3-9-policy-schema` (on T6, and on T2 for the shared files)

**Files:** Edit `internal/domain/graph/definition.go` (`Policy *PolicyDefinition`),
new `internal/domain/graph/policy.go` (`validatePolicy`),
`internal/domain/graph/validation.go`, `internal/adapter/yamlgraph/dto.go`,
`internal/shell/fields.go` and `internal/agent/fields.go` (a `policy` Group
descriptor with `paths.write` as `PathList`, `network` and `shell` as `Enum`,
`commands` as `List`), their config parsers, `internal/fields/fieldstest`,
`internal/adapter/yamlgraph/edit`, `docs/graph-spec.md`.

- [ ] Validation naming node and key: unknown keys, wrong types, bad
  patterns (T6 rules), node value widening a default, `policy` in a Gate,
  Human or embedded Gate config, `policy.defaults` carrying `secret_env`
  (closes F3.8-4), invalid `mode`.
- [ ] Hash: an absent `policy` is byte-identical (T2 golden re-run); a present
  one is semantic content (changing it changes the semantic version).

**Tests (red first):** `TestValidatePolicyNodeWidensDefault`,
`TestValidatePolicyUnknownKeysAndTypes`, `TestValidatePolicyOnGateAndHumanRejected`,
`TestPolicyIsSemanticHashContent`, `TestPolicyRoundTripAndEditorPreservesIt`,
`fieldstest` parity.

---

### Task 8: Runtime policy pipeline, audit mode, secret admission

**Branch:** `claude/f3-9-runtime` (on T3, T7; needs `claude/f3-1-runtime`)

**Files:** Create `internal/runtime/policy.go`, tests. Edit
`internal/runtime/scheduler.go` (`Options.Policy PolicyEnforcer`,
`Options.PolicyMode`, `ExecutionObservation.Policy`), `internal/runtime/pipeline.go`
(replace T3's default `admit`), `internal/runtime/batch.go`, `accept.go`,
`internal/runtime/isolation.go` (emission point after `workspace_prepared`),
`internal/runtime/AGENTS.md`.

```go
type PolicyEnforcer interface {
    Capabilities(ctx context.Context) policy.Capabilities   // cached, bounded, no side effects
    // Prepare is called by the executor at launch, never by the runtime.
}
```

The runtime only needs `Capabilities` and the pure decision; launching is the
executor's job (T10), so the port stays tiny and `internal/runtime` imports no
platform package.

- [ ] Pipeline order (T3's test now uses the real policy step): evaluate ->
  probe -> decide -> deliver-or-deny secrets -> resolve admitted -> redaction
  set -> report -> `run_started`.
- [ ] Enforce fail closed: any requested path/network/shell/commands whose
  state is `unenforceable`, `unobserved` or `detected` is
  `policy_unenforceable` naming node, permission and what would fix it.
  `delegated` and `contained` pass. A secret-holding node in enforce mode must
  declare `network` (`policy_secret_network_undeclared`). `secret_policy_denied`
  when policy denies delivery. Ambient bypass keeps failing in both modes.
- [ ] Audit: nothing blocked; `policy_evaluated` per node run with
  `applied: false`; observable violations (post-run scope check for Agent nodes
  with `paths.write`, `secret_in_workspace`) become `policy_violation` with
  outcome `recorded`; node outcome and run result unchanged.
- [ ] Enforce violations from checks the runtime performs itself end the node
  with failure kind `policy_blocked` (new `run.FailureKind` next to
  `FailureNeedsIntervention`, run class needs-intervention, exit code 4 per the
  spec's recommended default), a `policy_violation` with outcome `blocked` and a
  red `execution_policy` Check Result. A blocked node is not retried by the
  correction loop and is not recorded as accepted values; siblings untouched
  (follow the existing `accept.go` exhaustion path).
- [ ] `policy_evaluated` for an isolated Agent is emitted by the coordinator at
  dispatch (decision 5). It carries `redaction_values` as a count.

**Tests (red first):** `TestEnforceLandlockUnavailableFailsPreflight`
(fake capabilities), `TestAuditRecordsViolationsAndChangesNothing`,
`TestSecretPolicyDeniedNeverInEnvironment` (fake executor asserts the
environment), `TestPreflightSecretAmbientBypassEnforce`,
`TestPolicyBlockedNodeIsNotRetried`, `TestPolicyBlockedLeavesSiblingsAccepted`
(scheduler-level, fake executors, barriers), `TestPolicyEvaluatedBeforeFirstAttempt`
(event order, isolated and not), `TestPolicyAbsentEmitsNothing`,
`TestEventStreamReconstructsEvaluatedViolationFailure`, byte-identical
no-policy test, `-race -count=20` on the scheduling tests.

---

### Task 9: Permissions report and CLI

**Branch:** `claude/f3-9-cli` (on T8; after `claude/f3-1-cli`)

**Files:** Create `cmd/agentplane/permissions.go`, tests. Edit
`cmd/agentplane/cli.go` (new subcommand `graph permissions PATH [--json]`,
`run --policy-mode audit|enforce` as `fs.String`, usage string at the line that
prints it), `cmd/agentplane/serve.go` (compose the same options; no new route),
`cmd/agentplane/AGENTS.md`, `docs/features/F0.7-cli-runner.md` (flags),
`docs/graph-spec.md`.

- [ ] `agentplane graph permissions` evaluates policy and probes the platform
  but resolves no secret and reads no environment: assigned secrets show names
  and `resolved: not checked`. It prints type, requested permissions, assigned
  secret names and, per permission, state and `applied`, in node-ID order.
  `--json` uses `policy.Report`. Exit code like `graph validate`.
- [ ] `agentplane run` prints the same report to stderr before `run_started`
  (suppressed with `--json`, where it is part of the document); a graph without
  `policy` or `secrets` prints nothing new. `--policy-mode` can only
  strengthen; an invalid value is a usage error.
- [ ] CLI text never says "sandboxed"; it uses the six state words and, in
  audit, "would be" wording (decision 3).

**Tests:** `TestGraphPermissionsJSONStable` (golden), `TestReportHoldsNoSecretValue`,
`TestGraphPermissionsDoesNotReadEnvironment` (sets a poisoned variable, asserts
it is never looked up through an injected lookup), `TestRunPolicyModeCannotBeLowered`,
`TestRunPrintsReportBeforeRunStarted`, usage-string test, built-binary e2e on the
three-node fixture (exit codes), `serve` test that responses stay secret-free.

---

### Task 10: Launcher core (shell, commands, blocked outcome) and Windows

**Branch:** `claude/f3-9-launcher` (on T8; rebase on F3.4 helper if merged)

**Files:** Create `internal/adapter/sandbox/{doc,launcher,resolve}.go`,
`launcher_windows.go`, `launcher_other.go` with tests. Edit
`internal/shell/executor.go` (`Executor.Launcher` optional field, consumer-owned
`type Launcher interface{ Prepare(cmd *exec.Cmd, p policy.NodePlan) (func(), error) }`;
zero value keeps today's behavior), `cmd/agentplane/adapters.go` and `cli.go`
(compose `sandbox.Launcher`), `internal/archtest` (T5 rule now covers the
package), `docs/security-model.md`.

- [ ] **Resolve once.** The launcher resolves `command` to one absolute path:
  a bare name through the runner `PATH`; a name containing a separator against
  the node's resolved absolute `cwd` (`exec.Cmd` otherwise resolves it against
  `Dir` at exec time, which is not what a check against the runner cwd saw);
  `EvalSymlinks`; reject UNC, `\\?\` and alternate data streams on Windows;
  compare case-insensitively on Windows. The same absolute path is written to
  `cmd.Path` and `cmd.Args[0]` stays the node's argv. A `PATH` supplied by the
  node's own `env` cannot affect resolution (the runner `PATH` is used).
- [ ] `shell: off` and a non-matching `commands` entry: `Prepare` returns a
  typed refusal before `cmd.Start`; the executor maps it to
  `ExecutionError{Kind: FailurePolicy}` (new kind), the runtime turns that into
  `policy_blocked` (T8). The process is never started.
- [ ] `Prepare` is a no-op in audit (it still returns the would-be refusal as a
  recorded violation through the same typed error flagged `Recorded`).
- [ ] Windows: `shell` and `commands` enforcement and the full secret model.
  `paths.write` and `network: off` are `unenforceable` (enforce fails
  preflight) and `unobserved` (audit). No Job Object here; F3.4a owns process
  trees and the launcher stays compatible with either `CREATE_SUSPENDED` or
  `JOB_LIST` because it only sets `cmd.Path`.

**Tests (red first):** `TestLauncherNeverStartsRefusedCommand` (marker file the
command would create, asserted absent), `TestLauncherChecksAndLaunchesSameAbsolutePath`
(a decoy earlier in the node's own `PATH`), `TestLauncherResolvesRelativeCommandAgainstCwd`,
`TestWindowsLauncherPathForms` (UNC, `\\?\`, ADS, case, separators),
`TestWindowsDecisionTable`, `TestLauncherAuditRecordsButStarts`, runtime test
`TestPolicyBlockedNodeIsNotRetried` re-run with the real launcher. Windows
tests run in the existing `check (windows-x64)` job (non-blocking today); the
decision table and path-form tests are pure and run on Linux too.

---

### Task 11: Linux Landlock, user and network namespaces, real probe

**Branch:** `claude/f3-9-linux-enforce` (on T10)

**Files:** Create `internal/adapter/sandbox/{landlock_linux,helper_linux,netns_linux,probe_linux,scratch}.go`,
`*_other.go` stubs, tests with `TestMain` dispatching the helper. Edit
`cmd/agentplane/main.go` (dispatch the hidden helper subcommand before flag
parsing), `docs/security-model.md`.

- [ ] **Probe (real, bounded, cached per run).** Landlock: raw
  `landlock_create_ruleset(NULL, 0, VERSION)` (the `/sys/kernel/security/lsm`
  file does not exist on every kernel; the spike confirmed the syscall is the
  only reliable probe); record the ABI. Namespaces: start the helper in
  `CLONE_NEWUSER|CLONE_NEWNET` against a listener the probe owns and require
  `connect` to fail with a network-unreachable error; a successful connect or a
  failed clone both mean `unenforceable`. Never read or change a sysctl.
  Timeout 2 s, output bounded. In tests the helper is the test binary.
- [ ] **Helper.** The runner re-executes itself with a hidden subcommand. It
  locks the OS thread, sets `PR_SET_NO_NEW_PRIVS`, builds a ruleset handling
  write-class rights only (masked to the probed ABI, minimum ABI 1 in enforce),
  adds path-beneath rules for each resolved allowed directory, for the per-node
  scratch directory and a file-rights-only rule for `/dev/null` (a directory
  right on a non-directory returns `EINVAL`), calls `landlock_restrict_self`,
  verifies, then `syscall.Exec`s the resolved command from the same thread. Any
  failure exits non-zero without exec. It accepts only a versioned,
  length-prefixed argv with a checksum and rejects unknown flags and a missing
  or tampered field; Landlock only narrows authority, so a forged invocation
  cannot widen anything, and the tests assert the child cannot regain
  privileges.
- [ ] **Scratch.** For a node with `paths.write`, create a 0700 per-node
  directory under the user cache dir, set `TMPDIR`, `XDG_CACHE_HOME` and
  `GOCACHE` into it unless the node set them, allow writes there, remove it
  after the node. The report lists the exact writable set (resolved roots,
  scratch, `/dev/null`). `/tmp` and `$HOME` become unwritable, which the docs
  say plainly.
- [ ] **Namespaces.** `Cloneflags: CLONE_NEWUSER|CLONE_NEWNET` on the helper
  (the spike showed `CLONE_NEWNET` alone fails with `EPERM`). Verify the
  identity mapping (`uid -> uid`) works unprivileged; fall back to
  `0 -> uid` and document the difference if it does not. The two mechanisms
  compose (inherited domains); add a test combining them because the spike did
  not.
- [ ] Failure after preflight said `prevented` (changed kernel setting): the
  node fails with `policy_blocked` and `reason: enforcement_unavailable`; it
  never starts unconfined.

**Tests (red first):** `TestLandlockShellWritesOnlyAllowed` (writes `out/a.txt`
ok; `../escape.txt` and an absolute outside path fail; asserted on the
filesystem), `TestLandlockDevNullAndScratchWritable`, `TestLandlockHelperRejectsTamperedArgv`,
`TestLandlockChildCannotRegainPrivileges`, `TestNetnsOffRefusesLoopback`,
`TestNetOnConnects`, `TestLandlockAndNetnsCompose`, `TestProbeIsRealAndBounded`
(fake clock for the bound), `TestHelperLeavesNoChild` (wait on the process, not
a sleep), `TestEnforceFallsClosedWhenHelperFails`. Tests guarded by
`testenv`-style helpers skip with a reason where the mechanism is absent; the
CI job in T13 makes those skips failures.

---

### Task 12: Agent states and Codex sandbox pinning

**Branch:** `claude/f3-9-agent` (on T8 and F3.1 runtime)

**Files:** Edit `internal/agent/codex/adapter.go` (`args` at the line that
builds `--sandbox workspace-write`), `internal/agent/codex/adapter_test.go`,
`internal/runtime/policy.go`, `docs/validation/F3.9-codex-sandbox-spike.md`,
`docs/features/F3.9-*.md` (open questions closed).

- [ ] **Spike S2 first (15a).** On a disposable repository and a linked
  worktree record whether `--sandbox workspace-write` with an explicit
  network-off setting denies tool-command network, confines writes to the
  `--cd` root, and how it treats secret-like variable names (also closes F3.8
  question 6 if not done in S1). If the setting cannot be pinned, Agent
  `network: off` is `unenforceable` and enforce rejects it.
- [ ] Agent states per the matrix: `network` `delegated` (pinned) or
  `unenforceable`; `paths.write` `contained` plus `detected` with
  `isolation: worktree`, `detected` only without it (enforce then requires
  isolation: `TestEnforceAgentPathsWriteRequiresWorktree`); `shell` and
  `commands` `unenforceable`.
- [ ] Enforce, isolated Agent writing outside `paths.write`: F1.8 check on the
  worktree diff blocks the node before `Snapshot`/`Integrate`, so the unit is
  not integrated and `refs/agentplane/<run>/integrated` is unchanged.

**Tests (red first):** `TestIsolatedAgentOutOfScopeNotIntegrated` (real Git,
fake adapter writing inside its worktree; assert source checkout and integrated
ref, sorted evidence), `TestCodexArgsPinSandboxNetworkExplicitly`,
`TestAgentPolicyStatesMatrix`, `TestAuditAgentOutOfScopeRecordedOnly`.

---

### Task 13: CI evidence, docs closure and ADR number

**Branch:** `claude/f3-9-closure` (last)

**Files:** Edit `.github/workflows/ci.yml` (new job `enforce-linux`),
`docs/features/F3.8-*.md`, `docs/features/F3.9-*.md` (status, tick criteria
only for what a named test proves), `docs/features/README.md`,
`docs/adr/00XX-*.md` -> `docs/adr/00NN-*.md` (number at merge) and
`docs/adr/README.md`, `docs/roadmap.md`, `docs/graph-spec.md`,
`docs/security-model.md`, `docs/architecture.md`, `docs/codebase-map.md`,
`internal/runtime/AGENTS.md`, `cmd/agentplane/AGENTS.md`, `AGENTS.md` (phase
paragraph), `docs/validation/F3.8-F3.9-closure.md`.

- [ ] CI job `enforce-linux` on a runner or container configured explicitly
  for user namespaces and Landlock, running the T11 and T12 enforce tests with
  `-run` selecting them and a step that fails if any reports `SKIP`. A first
  step prints the probe output (Landlock ABI, userns result, kernel) so the
  first run documents real values; `ubuntu-latest` behavior (AppArmor
  restriction of unprivileged user namespaces) is unverified and recorded as
  unknown until that job runs. The workflow never relaxes a sysctl as part of
  a product test; if the job needs it, it is done in an explicit workflow step
  and named in the PR.
- [ ] Honesty rules: a mechanism is marked implemented only if its enforce
  test ran green on a named platform. Otherwise the progress log says
  "enforcement unverified on <platform>" (macOS and Windows paths/network are
  stated as unenforceable, not as untested).
- [ ] Consistency: `docs/security-model.md` replaces the "eventually
  `secret_ref`" text with the shipped contract; graph spec documents `secrets`,
  `secret_env`, `policy`, `config.policy`, the vocabulary and the platform
  matrix; docs never use "sandboxed" for a permission.
- [ ] Final review pass with the `code-review` skill on the combined diff, then
  `make check` and `go test -race ./...`, and the real-Codex recipe from S1/S2
  once more if Codex is available.

---

## Risks and edge cases

- **Redaction is exact-byte plus listed encodings.** Hashes, truncation, other
  encodings and MIME wrapping defeat it; the CLI help and docs say so. Larger
  sets slow the per-byte scan; T4a records a benchmark and enforces bounds.
- **Landlock scratch and caches.** `go build` under `paths.write` needs a
  writable cache; the launcher provides one in scratch, so builds are cold.
  Tools that need `$HOME` or `/tmp` fail with `EACCES`; the report and docs
  list the writable set.
- **User namespaces on `ubuntu-latest`.** AppArmor may restrict them; the
  probe must be real. A node that works locally can be `unenforceable` in CI,
  which is correct, not a bug.
- **Relative command and `cwd`.** `exec.Cmd` resolves a command containing a
  separator against `Dir` at exec time; checking it against the runner cwd is a
  confused-deputy bug. T10 resolves against the resolved `cwd`.
- **Absolute paths in events.** `scope_root` and the report print resolved
  absolute paths (as `workspace_prepared` already does); they are runtime
  output, not repo content.
- **Event schema collision.** F2.8b reserves 12, F3.4 proposes one; T1 takes
  the next free number at merge and F3.2's future-schema fixture must not use a
  literal.
- **Rebase conflicts.** `preflight.go`, `scheduler.go` and `cli.go` are edited
  by F3.1, F3.4 and this plan; each task keeps its logic in a new file and a
  single call line in the shared one.
- **`serve` write trust.** A local actor that can write graphs can request any
  `env/...` secret; the permissions report for `serve` is a recorded follow-up.
- **Codex behavior can change with the installed version.** `delegated` is only
  as strong as the vendor sandbox; S2 pins the version and flags it in output.
- **Helper hardening.** The re-exec path is reachable by anyone who can run the
  binary; it must only narrow authority and exit without exec on any doubt.

## Definition of done

Every F3.8 and F3.9 acceptance criterion maps to a named, passing test in the
table above (platform tests that skipped are not verification and are recorded
as unverified); `make check` and `go test -race ./...` pass; byte scans of the
store, outputs and API bodies find no secret; the architecture test proves
providers and platform enforcement are imported only by `cmd/agentplane`; the
ADR has its number and the docs state exactly what is prevented, contained,
delegated, detected, unobserved or unenforceable on each platform.
