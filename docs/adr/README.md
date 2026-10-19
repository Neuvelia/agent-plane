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
