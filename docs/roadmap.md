# Product roadmap - compact repository version

This is the repository's engineering index. IDs, titles, priorities and phase
exit criteria follow `docs/reference/full-roadmap-features.md` and the supplied
product roadmap PDF. The **dependency column is proposed implementation
sequencing**, inferred from feature descriptions; it is not a contractual
claim in the source roadmap. Review it when each feature is activated. An
active feature needs a measurable spec in `docs/features/`, and a new public
or durable contract needs an ADR before implementation. Do not generate later
feature specs solely to fill this index.

## Phase 0 - Execution Core (implemented)

Goal: prove graph/node/edge/run semantics without UI.

- F0.1 Graph Definition Schema v0
- F0.2 Graph Validator and lint
- F0.3 Explicit data bindings
- F0.4 In-memory DAG scheduler
- F0.5 Shell Node
- F0.6 Event Model and execution IDs
- F0.7 CLI Runner

Exit: a three-node graph runs independent branches concurrently, joins them, passes explicit data, emits structured events, and exits deterministically.

## Phase 1 - Agent Loop (implemented; real-Codex F1.7 demo executed 2026-11-17, `docs/validation/F1.7-real-codex-demo.md`)

Goal: execute one real coding agent behind an adapter and add deterministic correction.

| ID | Feature | Priority | Proposed dependencies | Spec |
| --- | --- | --- | --- | --- |
| F1.1 | Agent Adapter Contract | P0 | F0 exit | Implemented; ADR-0009 |
| F1.2 | First coding-agent adapter | P0 | F1.1 | Implemented; ADR-0010; real smoke verified |
| F1.3 | Unified Streaming | P0 | F1.1, F1.2 | Implemented; ADR-0011 |
| F1.4 | Gate Node | P0 | F0 exit; F1.2 for Agent demo | Implemented; ADR-0012 |
| F1.5 | Check Result Contract | P0 | F1.4 | Implemented; ADR-0013 |
| F1.6 | Correction Loop + maxAttempts | P0 | F1.1, F1.4, F1.5 | Implemented; ADR-0014 |
| F1.7 | Return Envelope / scoped retry | P0 | F1.5, F1.6 | Implemented; ADR-0015 |
| F1.8 | Changed-files Scope Check | P1 | F1.2, F1.4 | Implemented; ADR-0016 |

Exit: an agent modifies a repository, a deterministic check rejects a bad
result with reason/evidence/scope, and only the failed unit is corrected in no
more than three attempts. F1.8 (P1) adds the Git-derived enforcement that
F1.4's reported-path check does not provide. F1.7 preserves accepted runtime outputs; F3.1 adds
filesystem/workspace isolation.

## Phase 2 - Local IDE

Goal: move graph authoring and execution debugging from terminal to a local IDE.

| ID | Feature | Priority | Proposed dependencies |
| --- | --- | --- | --- |
| F2.1 | Graph Canvas | P0 | F0 graph schema |
| F2.2 | Node Inspector | P0 | F2.1 |
| F2.3 | Edge/Data Mapping Editor | P0 | F2.1, F0.3 |
| F2.4 | Graph Validation UI | P0 | F2.1-F2.3, F0.2 |
| F2.5 | Live Execution Map | P0 | F1.3, F2.1 |
| F2.6 | Run Inspector - Overview | P0 | F0.6, F1.5 (spec: implemented in the renderer; gap spec) |
| F2.7 | Run Inspector - Node Detail | P0 | F2.6, F1.6 (spec: accepted for implementation, remaining gap only) |
| F2.8 | Attempt Compare | P1 | F2.7, F1.6 |
| F2.9 | Local Run History + SQLite | P0 | F0.6, F2.6 |
| F2.10 | Human Gate UI | P0 | F2.5, F2.6; local pause/approval contract |
| F2.11 | Onboarding + Capability Detection | P1 | F1.1, F1.2 |
| F2.12 | Graph Templates | P1 | F2.1-F2.4 |

F2.6 and F2.7 are largely delivered through F2.5, F2.5a, F2.8a, F2.9 and F2.3-E6; their
specs in `docs/features/` list what is already delivered and cover only the remaining gap.

Exit: a developer authors and runs a graph, watches execution live, inspects
reason/evidence, handles a human gate and returns to an older run without a
terminal except for launching the application. The F2.10 local approval
contract is ADR-0017 (implemented).

## Phase 3 - Local Production Ready

Goal: make long-running local execution durable and safer.

| ID | Feature | Priority | Proposed dependencies |
| --- | --- | --- | --- |
| F3.1 | Workspace Isolation / Git Worktrees | P0 | F1.7; closes file-state gap. Implemented; ADR-0024 |
| F3.2 | Durable Run State / Append-only Event Store | P0 | F0.6, F1.3 (spec: implemented on Linux) |
| F3.3 | Resume / Recovery | P0 | F3.1, F3.2 |
| F3.4 | Cancellation and Timeouts | P0 | F1.2, F1.6; F3.2 only for the durable-store criteria |
| F3.5 | Artifact Store Local | P0 | F1.2, F3.2 |
| F3.6 | Graph Versioning and Run Reproducibility | P0 | F0.6, F3.2 |
| F3.7 | Export JSON/CSV | P0 | F3.2, F3.5 |
| F3.8 | Secrets References | P0 | F1.1, F3.1; spec: implemented for the `env` provider and Shell nodes (ADR-0029 proposed; Agent `secret_env` and keychain not included) |
| F3.9 | Permissions / Execution Policy v0 | P0 | F1.2, F3.1, F3.8 |
| F3.10 | Cost/Usage Metadata | P1 | F1.1, F3.2 |
| F3.11 | Local Diagnostics | P1 | F3.2, F3.4 |

Exit: a run can stop and resume after restart, parallel nodes use isolated
workspaces, trace/artifacts are durable, and secrets and permissions have an
explicit model. F3.1 (implemented, opt-in per Agent node) establishes
file-level isolation that F1.7 does not promise; it is not a sandbox (F3.9),
downstream nodes do not yet see upstream files, and Shell nodes are not isolated. Design F3.8 and F3.9 together so secret resolution never bypasses
the execution policy.

## Phase 4 - Connected Control Plane

Goal: remotely supervise local runners without requiring repository code in cloud storage.

| ID | Feature | Priority | Proposed dependencies |
| --- | --- | --- | --- |
| F4.1 | Account + Authentication | P0 | F3 exit |
| F4.2 | Projects | P0 | F4.1 |
| F4.3 | Runner Enrollment | P0 | F4.1, F4.2 |
| F4.4 | Outbound Secure Connection | P0 | F4.3 |
| F4.5 | Runner Presence & Health | P0 | F4.4 |
| F4.6 | Remote Run Start | P0 | F4.2, F4.4, F4.9 security baseline |
| F4.7 | Cloud Run Metadata Sync | P0 | F3.2, F4.4, F4.6 |
| F4.8 | Web Human Approval | P0 | F2.10, F4.4, F4.7, F4.9 |
| F4.9 | Cloud Security Baseline | P0 | F4.1-F4.4; before remote commands |
| F4.10 | Optional Artifact Sync | P1 | F3.5, F4.7, F4.9 |

Exit: a user pairs a laptop as runner, sees its status, starts a graph from
the web, watches run metadata and approves a human gate. Repository contents
may remain entirely local.

## Phase 5 - Pro / Team

Goal: monetize continuity, collaboration, policy, and history.

| ID | Feature | Priority | Proposed dependencies |
| --- | --- | --- | --- |
| F5.1 | Plans + Billing | P0 | F4.1, F4.2 |
| F5.2 | Scheduling | P0 | F4.6, F4.9 |
| F5.3 | Alerts / Notifications | P0 | F4.7, F5.2 |
| F5.4 | Long-term History & Analytics | P0 | F4.7 |
| F5.5 | Organizations / Team Workspaces | P0 | F4.1, F4.2 |
| F5.6 | RBAC | P0 | F5.5 |
| F5.7 | Shared Graph Registry | P0 | F3.6, F5.5, F5.6 |
| F5.8 | Shared Constraints / Learning Edge v1 | P1 | F5.6, F5.7 |
| F5.9 | Policy Engine v1 | P1 | F3.9, F5.6 |
| F5.10 | Audit Log | P0 | F5.5, F5.6; before team actions |
| F5.11 | Budgets & Guardrails | P1 | F3.10, F5.1, F5.9 |
| F5.12 | Git Provider Integration | P1 | F4.2, F5.6, F5.10 |

Exit: Pro/Team supports paid plans, schedules and alerts, shared history,
organizations, RBAC and audit. A team can control and inspect its agents as a
shared system.

## Phase 6 - Managed execution / enterprise

Only after the control-plane value is proven. Every F6 item is P2.

| ID | Feature | Priority | Proposed dependencies |
| --- | --- | --- | --- |
| F6.1 | Managed Ephemeral Runners | P2 | F3.1, F3.9, F4.9, F5.1 |
| F6.2 | Managed Artifact Storage | P2 | F3.5, F4.10, F6.1 |
| F6.3 | Enterprise Identity | P2 | F4.1, F5.5, F5.6 |
| F6.4 | Advanced Policy / Approval Chains | P2 | F3.9, F4.8, F5.9, F6.3 |
| F6.5 | Plugin/Adapter SDK | P2 | F1.1/F1.2 contract proven, F5.7 |

Exit: managed execution is isolated and measurable, while enterprise features
do not complicate the local-first core for an individual developer.
