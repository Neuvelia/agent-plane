# Thesis experiment notes

Working thesis topic: deployment of AI agent systems to production environments.

AgentPlane can serve both as the engineering artifact and as instrumentation for experiments.

## Candidate research questions

- Does deterministic validation plus bounded local correction improve task success rate?
- Does graph-based parallel execution reduce wall-clock time for independent units compared with a sequential pipeline?
- How does retry limit affect time, cost, and success probability?
- Does retrying only the failed unit reduce unnecessary changes compared with retrying the whole batch?
- Do deterministic gates reduce required human interventions?

## Suggested experiment variants

```text
A baseline:       sequential, no deterministic gate, no local retry
B validated:      sequential, deterministic gate, bounded retry
C orchestrated:   graph, deterministic gate, scoped retry, parallelism
D controlled:     C + human gate for high blast radius work
```

## Metrics

Capture when available:

- success/failure;
- wall-clock duration;
- total agent execution duration;
- time-to-green;
- attempts;
- agent/model calls;
- human interventions;
- changed file count;
- changes outside declared scope;
- test/check results;
- token/usage metadata;
- rollback/rejection events.

## Development-process measurement

The implementation process itself can provide secondary data.

For each Codex-assisted feature, optionally record:

- feature ID;
- plan session date;
- implementation session date;
- number of correction cycles;
- whether `make check` passed on first final attempt;
- review findings by severity;
- human interventions;
- files/lines changed;
- merge outcome.

Use `experiments/run-log.csv` as a minimal starting schema.
