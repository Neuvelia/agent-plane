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

F2.10: [ADR-0019 - Local Human gate contract](0019-local-human-gate-contract.md) (proposed).
