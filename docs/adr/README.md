# Architectural Decision Records

Use an ADR for decisions that are expensive to reverse or that change a durable contract.

Examples:

- YAML library and strictness policy if it becomes a public parsing contract;
- event storage model;
- graph version/hash semantics;
- workspace isolation strategy;
- local/cloud trust boundary;
- persistence technology;
- process sandbox strategy.

Do not create ADRs for ordinary implementation details.

Number ADRs sequentially and keep superseded decisions rather than rewriting history.

F1.4: [ADR-0012 - Deterministic Gate node and restricted comparison command](0012-gate-node-contract.md).

F1.5: [ADR-0013 - Versioned Check Result and typed trace retrieval](0013-check-result-contract.md).

F1.6: [ADR-0014 - Bounded node-local correction attempts](0014-bounded-node-correction-loop.md).

F1.7: [ADR-0015 - Static units, Return Envelopes and immutable acceptance](0015-return-envelope-and-unit-acceptance.md).

F1.8: [ADR-0016 - Git-observed changed-files scope checking](0016-changed-files-scope-check.md).

F1.4b: [ADR-0042 - Test command as a loop check](0042-test-command-loop-check.md) (accepted 2026-11-27).

Phase 2 vertical slice (accepted):
[ADR-0017 - Human node and local approval](0017-human-node-and-local-approval.md),
[ADR-0018 - Correction loop for Shell producers](0018-shell-node-correction-loop.md),
[ADR-0019 - Local JSONL run store and loopback API](0019-local-run-store-and-loopback-api.md).

Graph editor (ADR-0020 to ADR-0022 accepted, ADR-0023 accepted; plan `docs/superpowers/plans/2026-11-14-graph-editor.md`):
[ADR-0020 - Graph edit operations on the YAML tree](0020-graph-edit-operations.md),
[ADR-0021 - Local graph document API and write trust](0021-local-graph-document-api-and-write-trust.md),
[ADR-0022 - Canvas layout stored in `metadata.layout`](0022-graph-layout-metadata.md),
[ADR-0023 - Recorded run values for edge inspection](0023-recorded-run-values.md)
(accepted; recording is opt-in, off by default).

F2.8 Attempt Compare (accepted 2026-11-19 for the F2.8b engine capture; opt-in `--record-diffs`):
[ADR-0026 - Attempt diff capture for Attempt Compare](0026-attempt-diff-capture.md).

Onboarding (accepted): [ADR-0025 - Read-only capability report on the loopback API](0025-capability-report.md).

Phase 3:
[ADR-0024 - Workspace isolation with Git worktrees and deterministic integration](0024-workspace-isolation-with-git-worktrees.md)
(accepted; real-Codex sandbox spike passed; implemented by F3.1, with a real-Codex run recorded in
`docs/validation/F3.1-real-codex-demo.md`).

F3.2 durable run state (accepted 2026-11-26):
[ADR-0027 - Durable run state on an append-only JSONL log](0027-durable-run-state.md).

F3.4 cancellation and timeouts (proposed):
[ADR-0028 - Cancel and timeout reasons, cross-platform process ownership](0028-cancel-timeout-reasons-and-process-ownership.md).

F3.8 secret references and F3.9 execution policy (accepted 2026-11-26; F3.9 not implemented):
[ADR-0029 - Secret references and execution policy v0](0029-secret-references-and-execution-policy.md).

Owner-decision gaps (Q17, 2026-11-26; all proposed, none implemented):
[ADR-0039 - Requested permissions in the desktop](0039-requested-permissions-in-desktop.md) (F3.9c),
[ADR-0040 - JSON field selection in data bindings](0040-json-field-selection-in-bindings.md) (F2.3b),
[ADR-0041 - Workspace isolation for Shell nodes](0041-shell-node-workspace-isolation.md) (F3.1c).

F2.10: [ADR-0043 - Human reject routing and approval context](0043-human-reject-routing-and-gate-context.md) (accepted 2026-11-27).

Release engineering (proposed):
[ADR-0030 - Distribution channels, desktop shell and release tooling](0030-distribution-and-release-channels.md),
[ADR-0031 - Public roadmap status feed and landing synchronization](0031-public-roadmap-status-feed.md).

Roadmap decisions from the 2026-11-25 audit (accepted 2026-11-26; index in
`docs/roadmap/decisions/README.md`):
[ADR-0032 - Canonical vocabulary: CLI name, state model and event names](0032-canonical-vocabulary-cli-states-events.md),
[ADR-0033 - Canonical graph format, graph version and JSON input](0033-canonical-graph-format.md),
[ADR-0034 - Canonical Agent adapter interface](0034-canonical-agent-adapter-interface.md),
[ADR-0035 - Runtime stack and repository layout](0035-stack-and-repository-layout.md),
[ADR-0036 - Roadmap precedence for phase scope; units, blast radius and ordering](0036-roadmap-scope-precedence.md),
[ADR-0037 - Direction: a test command as a loop check](0037-test-command-check-direction.md),
[ADR-0038 - Roadmap tracking harness](0038-roadmap-tracking-harness.md).

Follow-ups to the audit (accepted 2026-11-27, implemented; ADR-0042 is listed above):
[ADR-0044 - Event schema 15: run provenance, test_command checks and abandoned executors](0044-event-schema-15-run-context.md) (F2.9, F3.6, F1.4b, F3.4),
[ADR-0045 - Bounded Shell capture and complete declared outputs](0045-shell-declared-output-completeness.md) (F0.5, amends ADR-0005).

Desktop presentation:
[ADR-0046 - The Inbox triage queue replaces the Dashboard](0046-inbox-replaces-dashboard.md) (F2.5b),
[ADR-0047 - Desktop light theme and profile menu](0047-desktop-light-theme-and-profile-menu.md) (F2.5c),
[ADR-0048 - Welcome and setup as a four-step setup](0048-four-step-setup.md) (F2.11, F2.12; proposed 2026-11-27),
[ADR-0049 - New run with a live preflight](0049-new-run-with-preflight.md) (F2.5e).

F1.2b: [ADR-0050 - Claude Code adapter: process permissions and evidence](0050-claude-code-process-permissions-and-evidence.md) (accepted 2026-11-27, pending spike).
