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

Phase 2 vertical slice (accepted):
[ADR-0017 - Human node and local approval](0017-human-node-and-local-approval.md),
[ADR-0018 - Correction loop for Shell producers](0018-shell-node-correction-loop.md),
[ADR-0019 - Local JSONL run store and loopback API](0019-local-run-store-and-loopback-api.md).

Graph editor (proposed, plan `docs/superpowers/plans/2026-11-14-graph-editor.md`):
[ADR-0020 - Graph edit operations on the YAML tree](0020-graph-edit-operations.md),
[ADR-0021 - Local graph document API and write trust](0021-local-graph-document-api-and-write-trust.md),
[ADR-0022 - Canvas layout stored in `metadata.layout`](0022-graph-layout-metadata.md),
[ADR-0023 - Recorded run values for edge inspection](0023-recorded-run-values.md)
(proposed; one product decision open: opt-in or default-on).
