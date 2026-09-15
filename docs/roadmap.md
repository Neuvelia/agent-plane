# Product roadmap - compact repository version

The detailed product roadmap lives outside this starter repository. This file keeps the engineering phases visible to coding agents and contributors.

## Phase 0 - Execution Core

Goal: prove graph/node/edge/run semantics without UI.

- F0.1 Graph Definition Schema v0
- F0.2 Graph Validator and lint
- F0.3 Explicit data bindings
- F0.4 In-memory DAG scheduler
- F0.5 Shell Node
- F0.6 Event Model and execution IDs
- F0.7 CLI Runner

Exit: a three-node graph runs independent branches concurrently, joins them, passes explicit data, emits structured events, and exits deterministically.

## Phase 1 - Agent Loop

Goal: execute one real coding agent behind an adapter and add deterministic correction.

- agent adapter contract;
- first coding-agent adapter, expected first target: Codex CLI;
- normalized streaming events;
- deterministic Gate Node;
- Check Result contract;
- bounded correction loop;
- return envelope / scoped retry;
- changed-files scope check.

Exit: an agent modifies a repository, a deterministic check rejects a bad result with reason/evidence/scope, and only the failed unit is corrected within a bounded attempt count.

## Phase 2 - Local IDE

Goal: move graph authoring and execution debugging from terminal to a local IDE.

- graph canvas;
- node inspector;
- edge/data mapping editor;
- validation UI;
- live execution map;
- run inspector;
- local run history;
- human gate UI;
- capability detection and starter templates.

## Phase 3 - Local Production Ready

Goal: make long-running local execution durable and safer.

- Git worktree isolation;
- durable append-only event store;
- recovery/resume;
- cancellation/timeouts;
- local artifact store;
- graph version snapshots;
- JSON/CSV export;
- secret references;
- execution permissions/policy v0;
- usage metadata and diagnostics.

## Phase 4 - Connected Control Plane

Goal: remotely supervise local runners without requiring repository code in cloud storage.

- accounts/projects;
- runner enrollment;
- outbound secure connection;
- remote run start;
- cloud run metadata;
- web approvals.

## Phase 5 - Pro / Team

Goal: monetize continuity, collaboration, policy, and history.

- scheduling;
- long-term history;
- team workspaces;
- RBAC;
- audit;
- policy engine;
- billing;
- shared runners and integrations.

## Phase 6 - Managed execution / enterprise

Only after the control-plane value is proven:

- managed ephemeral runners;
- stronger sandboxing;
- enterprise identity/policy;
- advanced analytics and governance.
