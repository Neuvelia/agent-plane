# ADR-0047 - Tool-less model adapter and tier ladder in the correction loop

**Status:** proposed
**Date:** 2026-11-27
**Feature:** F1.6b (issue #203)
**Amends:** ADR-0014 (the loop may switch adapters between attempts); conforms to ADR-0034

## Context

The F1.6 loop retries one adapter. Every adapter today (Codex, and Claude Code
in #202) is a full coding agent: it plans, runs tools and edits the workspace
itself. That is the right tool for open-ended work, but it is expensive and
slow for small, scoped edits, where a single chat completion often suffices.

The reference project in the adoption analysis
(`kacwojdev/agent-control-plane`, README, measurements dated 2026-10-02 to
2026-10-26) runs such units on cheap models and escalates only on red
verdicts. It reports:

- SEARCH/REPLACE output instead of whole files cut cost per accepted change
  from about 0.0117 to 0.0015 USD at equal acceptance;
- failures of cheap models at temperature 0 repeat with identical evidence;
- model output never reaching a shell is a safety property it relies on.

## Decision

1. **A tool-less `model` adapter** implements the canonical interface
   (ADR-0034) without changing it. One `Execute` call is one chat completion
   against an OpenAI-compatible `/chat/completions` endpoint, using the
   standard library HTTP client. No new dependency.
2. **The adapter, not the model, edits files.** The reply grammar is line
   based:

   ```text
   === EDIT relative/path ===
   <<<<<<< SEARCH
   exact existing text
   =======
   replacement text
   >>>>>>> REPLACE
   === END ===
   === FILE relative/new/path ===
   full content
   === END ===
   ```

   A reply is valid only if every block names a path in `allowed_paths`
   (`FILE` only for a path that does not exist yet), every SEARCH occurs
   exactly once, and paths contain no `..`, absolute prefix or symlink
   component. Validation completes before any write, and the writes are then
   applied as a whole (all or nothing within the process). An invalid reply
   writes nothing and is reported as a red Check Result of kind `edit_format`
   or `edit_out_of_scope`, attributed to the attempt as if an implicit
   pre-Gate had run. Those two kinds are reserved for the adapter boundary.
3. **Tiers belong to the loop**, not to the graph topology. `loop.tiers` is an
   ordered list of `{adapter, attempts, config}`. The sum of `attempts` equals
   `max_attempts`, so the single bound of ADR-0014 (at most 10) stays. A tier
   is selected by cumulative count. Two consecutive red results on one tier
   with equal evidence fingerprints (SHA-256 of the bounded, redacted evidence)
   end that tier early. Skipped attempts are not run and not counted.
   Feedback crosses tiers unchanged.
4. **Endpoints are runner configuration.** `--model-endpoint NAME=URL` and
   `--model-endpoint-key NAME=SECRET_REF` (F3.8). A graph names only
   `endpoint: NAME` and `model: STRING`, so graph files stay free of hosts and
   credentials and stay portable.
5. **Egress is explicit.** A host that does not resolve only to loopback
   addresses is remote. Using it requires `data_egress: remote` in that tier's
   config, checked at preflight. Before every remote request, the F3.8 secret
   scan runs over the outgoing content. A hit is an operational error and no
   request is made.
6. **Provider errors** (transport, HTTP 5xx, 429, timeout) get at most one
   retry inside `Execute`. If that fails too, the attempt ends with an
   operational error that does not consume a loop attempt and fails the node
   (no verdict). HTTP 4xx other than 429 is not retried.
7. **Events:** attempt events gain `tier`, `adapter`, `model` and, on a tier
   change, `escalation_reason` (`tier_exhausted` | `identical_evidence`). This
   is an additive schema bump; readers accept earlier schemas.

## Alternatives considered

### A. One graph node per tier, escalation by graph edges

Pros:

- No loop change.

Cons:

- Escalation needs "run on failure" edges, which schema v0 does not have.
- It violates "retry the smallest unit" (rule 6) by turning one unit into
  several.
- Feedback would have to travel as graph data.

### B. Only full agents, with a cheaper model passed to the agent CLI

Pros:

- No new adapter.

Cons:

- The agent's tool use and session overhead dominate the cost.
- Its edits are not confined by construction, only detected after the fact
  (F1.8).

### C. Whole-file replies

Pros:

- Simpler parser.

Cons:

- Measured worse in the reference project (more tokens, lost alignment, more
  failures).
- It rewrites unrelated lines.

### D. Native provider SDKs (Anthropic, OpenAI)

Pros:

- Richer features.

Cons:

- Adds dependencies.
- OpenAI-compatible endpoints already cover OpenRouter, vLLM, llama.cpp and
  Ollama.

## Consequences

### Positive

- Cheap first attempts with a deterministic path to a stronger adapter. Cost
  becomes a first-class, observable property (F3.10).
- Model output cannot write outside `allowed_paths` and is never executed.
- Local endpoints keep the feature fully local-first.

### Negative / trade-offs

- A second kind of Agent with different strength: tool-less models cannot run
  tests or explore. Authors must choose tiers deliberately, and docs must say
  which tasks suit them.
- `edit_format` and `edit_out_of_scope` are Check Results not produced by a
  Gate, a new attribution rule in the trace.
- Remote endpoints send repository content off the machine. That is mitigated
  by per-node consent and secret scanning, not eliminated.

## Compatibility and migration

Additive. Graphs without `loop.tiers` and without `adapter: model` keep their
semantic hash and behavior. Event readers accept the earlier schema.

## Validation

F1.6b acceptance criteria, plus one recorded real run against a local
endpoint and one against a remote endpoint, in `docs/validation/`. Each
reports the tier that accepted the result and the usage per attempt.
