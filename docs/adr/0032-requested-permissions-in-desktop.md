# ADR-0032 - Requested permissions in the desktop: read-only report route, inspector section and pre-run view

**Status:** proposed
**Date:** 2026-11-26
**Feature:** F3.9c (`docs/features/F3.9c-requested-permissions-ui.md`); amends scope statements of F2.2 and F3.9
**Closes:** issue #184 (owner decision Q17: a gap until an ADR decides)

## Context

Roadmap F2.2 lists a **permissions** section in the node inspector. Roadmap F3.9 acceptance 2 says the UI shows
the requested permissions before a run. Today neither exists: F2.2 drops permissions as a non-goal without naming
an owner (`F2.2-node-inspector.md`, Non-goals and "Out of scope"), and F3.9 defers the UI to an unnamed follow-up
and ships a CLI `agentplane graph permissions` command instead (`F3.9-execution-policy-v0.md`, "Out of scope").

F3.9 is a draft (its design is accepted in ADR-0029, 2026-11-26, shared with F3.8; the F3.8 `env` provider is
implemented, the F3.9 part is not): the `policy` graph surface, the pure policy evaluation and the permissions
report type are not implemented, and `policy_evaluated`/`policy_violation` events exist in schema 14 but are not emitted. This ADR therefore decides *how the desktop will surface what
F3.9 defines*, not what the permissions are. It does not duplicate or pre-empt ADR-0029.

Constraints that bind the decision:

- AGENTS.md rule: no graph rule in TypeScript; the renderer asks the engine (F2.1 requirement 5, F2.2 decision).
- F2.2 descriptors are generated from Go definitions; a permissions section must not be hand-written in the
  renderer.
- ADR-0021: write routes sit behind local write trust; a read-only route widens nothing.
- ADR-0029: audit mode never reports `applied: true`; the report carries an enforcement state per permission from
  a closed vocabulary. Anything the UI says about enforcement must come from that report, never from the renderer.
- Secret values must never reach the API or UI (F3.8). Only names and assignments.

## Decision (recommended; the owner chooses)

Recommended option **B**: expose the F3.9 requested-permissions report through a **read-only** loopback route and
show it in the desktop in two places, with **no editing** in the first slice.

1. A read-only route returns the F3.9 permissions report for the open graph text (the same Go function behind
   `agentplane graph permissions --json`; one implementation, two consumers). It evaluates against a
   caller-supplied policy mode (`audit` or `enforce`, default as the CLI) and the sidecar's own platform probe.
2. The node inspector gets a **Permissions** section, read-only, listing for the selected node the requested
   permissions (shell, commands, paths.write, network), their effective source (graph default, node override),
   the assigned secret names, and the enforcement state per permission with the closed vocabulary of ADR-0029.
3. The run-start flow shows a **pre-run permissions panel** summarising every node's requested permissions and
   states, and surfaces preflight blockers (`policy_unenforceable`, `secret_policy_denied`) before any process
   starts. The Run action is not blocked by the panel itself; blocking is the engine's preflight, as today.
4. Editing `policy` through the inspector is a later slice (F3.9c-S3), gated on this ADR being accepted and F3.9
   being implemented, and uses ordinary graph edit operations (ADR-0020) with descriptors; it needs no new ADR
   unless it adds an operation outside the versioned vocabulary.

## Alternatives considered

### A. Keep CLI only and document the gap

Pros: nothing to build; F3.9 already ships `graph permissions`.

Cons: breaks the roadmap acceptance "the UI shows the requested permissions before a run"; the desktop is the
primary surface and a user reviewing blast radius must leave it. The gap would remain unnamed.

### B. Read-only route plus inspector section plus pre-run panel (recommended)

Pros: satisfies both roadmap statements; one Go implementation shared with the CLI; no renderer policy logic; no
graph, schema or event change; secret-free by construction; safe to ship before editing exists.

Cons: users see permissions but must edit YAML (or the Advanced section) to change them until S3; a new route must
be kept in sync with the report type; adds a dependency on F3.9 being implemented first.

### C. Full editable permissions section now, descriptor-driven

Pros: complete roadmap reading of F2.2 in one step.

Cons: widens the editor before the F3.9 field registry and enforcement semantics are proven on real platforms;
risks presenting a permission as active when the platform cannot enforce it; larger blast radius for a P0 security
surface. Better as a follow-on to B.

### D. Show a summary computed in the renderer from `config.policy`

Pros: no route.

Cons: forbidden by the "no graph rules in TypeScript" rule and would invent enforcement states. Rejected.

## Consequences

### Positive

- The unnamed deferrals in F2.2 and F3.9 receive one target feature ID (F3.9c) with testable acceptance criteria.
- The UI cannot claim more than the engine report states (audit never `applied`).
- Windows and macOS users see `unenforceable`/`unobserved` honestly before a run.

### Negative / trade-offs

- F3.9c cannot start until F3.9 lands the report type; until then the gap remains and is recorded as blocked.
- Read-only first means a visible but non-editable section, which needs clear copy ("edit in YAML").

## Compatibility and migration

No graph schema change, no event change, no `schema_version` bump, no event schema number consumed. One additive
loopback route (read-only; no write trust needed). Existing graphs without `policy` show every permission as
"unrestricted (not declared)" (the F3.9 wording) with the platform's honest states. Older sidecars without the route: the desktop hides the
section rather than guessing.

## Validation

F3.9c acceptance criteria; in particular a Go test that the route output equals `graph permissions --json` for the
same graph, and a renderer test that no enforcement state is derived client-side. Not implemented by this ADR.
