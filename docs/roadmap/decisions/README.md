# Roadmap decisions (index)

Decisions are ADRs in `docs/adr/` (one numbering, ADR-0038). This page only
indexes the ones that settle roadmap questions: contradictions between the two
source documents (`docs/reference/full-roadmap-features.md`, the roadmap, and
`docs/reference/full-architecture-plan.md`, the architecture plan) and the
tracking process. Their combined result is `docs/roadmap/canonical-spec.md`.

| Audit decision | Topic | ADR |
| --- | --- | --- |
| D1, D2/D3, D4 | CLI name, node/run states, event names | [ADR-0032](../../adr/0032-canonical-vocabulary-cli-states-events.md) |
| D5, D15 | Graph format, graph version, JSON input | [ADR-0033](../../adr/0033-canonical-graph-format.md) |
| D6 | Agent adapter interface | [ADR-0034](../../adr/0034-canonical-agent-adapter-interface.md) (confirms ADR-0009) |
| D7/D8, D13, D14 | Phase scope precedence, units, blast radius, F2.7 before editor work | [ADR-0036](../../adr/0036-roadmap-scope-precedence.md) |
| D9, D10 | Go + Electron, `cmd/` + `internal/` layout, ownership areas | [ADR-0035](../../adr/0035-stack-and-repository-layout.md) |
| D11 | JSONL run store instead of SQLite | [ADR-0027](../../adr/0027-durable-run-state.md) (accepted 2026-11-26) |
| D12 | Test command as a loop check (gap to close) | [ADR-0037](../../adr/0037-test-command-check-direction.md) |
| Q2-Q10, Q17 | Tracking harness, status vocabulary, traceability | [ADR-0038](../../adr/0038-roadmap-tracking-harness.md) |
| Q13 | ADR-0027 and ADR-0029 accepted; ADR-0028 stays proposed until F3.4 closes | ADR-0027, ADR-0029 |
| D12 follow-up (#183) | Test command Gate kind, loop only (F1.4b) | [ADR-0042](../../adr/0042-test-command-loop-check.md) (accepted 2026-11-27) |
| Q17 (#187) | Human `on_reject: continue` only into graph outputs; approval context | [ADR-0043](../../adr/0043-human-reject-routing-and-gate-context.md) (accepted 2026-11-27) |
| #186, #183, #190 | Event schema 15: run provenance, `test_command`, `executor_abandoned`; canonical-spec §3 | [ADR-0044](../../adr/0044-event-schema-15-run-context.md) (accepted 2026-11-27) |
| #181 | Shell capture bound; declared outputs complete or failed | [ADR-0045](../../adr/0045-shell-declared-output-completeness.md) (accepted 2026-11-27) |
| Adoption analysis (#203-#207) | Tier ladder and tool-less model adapter (F1.6b), baseline/golden check preflight (F1.4c), fail-closed check confinement (F3.9d), agentctl v1 import (F0.1a), resume semantics (F3.3) | [ADR-0047](../../adr/0047-tool-less-model-adapter-and-tier-ladder.md), [ADR-0048](../../adr/0048-baseline-and-golden-check-preflight.md), [ADR-0049](../../adr/0049-fail-closed-check-confinement.md), [ADR-0050](../../adr/0050-agentctl-v1-graph-import-mapping.md), [ADR-0051](../../adr/0051-resume-and-recovery-semantics.md) (proposed) |
| Q17 (#184, #185, #189) | Requested permissions UI, JSON field selection, Shell isolation (specs only) | [ADR-0039](../../adr/0039-requested-permissions-in-desktop.md), [ADR-0040](../../adr/0040-json-field-selection-in-bindings.md), [ADR-0041](../../adr/0041-shell-node-workspace-isolation.md) (proposed) |

New roadmap decisions: create an ADR with `scripts/new-adr.sh <slug>`, then add a
row here and update `canonical-spec.md` if the decision changes it.
