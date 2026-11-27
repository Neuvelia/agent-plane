# ADR-0050 - Importing agentctl YAML v1 graphs into schema v0

**Status:** proposed
**Date:** 2026-11-27
**Feature:** F0.1a (issue #206)
**Respects:** ADR-0033 (schema v0 canonical; `depends_on`, `foreach`, `{{ }}` excluded), AGENTS.md rule 4 (edges are data dependencies)

## Context

The reference project of the adoption analysis
(`kacwojdev/agent-control-plane`) uses the architecture plan's §11 sketch as
its format ("agentctl YAML v1"). ADR-0033 rejected that sketch for this
repository. To lower the switching cost, a translator must map the old
constructs to schema v0 without importing the excluded semantics into the
runtime, and without silently weakening a graph.

## Decision

1. **Import is a one-way, offline translation** (`agentplane graph import
   --from agentctl-v1`). The runtime gains no parser, mode or semantics for
   the old format.
2. **Interpolation becomes data flow.** Every `{{ nodes.X.output.stdout }}` and
   `{{ inputs.k }}` inside `run` becomes:
   - a declared output (or graph input);
   - an edge (or graph input binding);
   - an `input_env` mapping;
   - a literal `"${AP_...}"` in the script.

   Values are no longer spliced into shell text. Each rewrite is listed in the
   report. Placeholders outside `run`, and any other placeholder form, are
   unsupported.
3. **Ordering-only `depends_on` becomes an `exit_code` edge.** When N depends
   on X but uses none of X's data, X declares `exit_code` and the edge
   `X.outputs.exit_code → N.inputs.after_X` (number, required) is added. The
   edge is a real data dependency in schema v0: X's failure blocks N, as in
   the source. The cost is that the F0.2 unused-input warning (#180) fires for
   `after_X`. The importer reports that warning as expected rather than
   suppressing it.
4. **Never weaker silently.** Source properties that schema v0 cannot enforce
   yet are refused unless `--allow-weaker` is given: `network: false` (F3.9)
   and `workdir: run` (F3.1c). With the flag, each is listed in the report and
   in `metadata.import.weakened`.
5. **Unsupported means no output.** `run_always`, `expect_status`, `foreach`
   over inputs, and `agent` / `remote` nodes give exit 3 with every location
   and the enabling feature. Literal `foreach` lists are expanded statically.
6. **Deterministic output** with provenance `metadata.import {format,
   source_sha256, name, weakened, blast_radius}`. Metadata does not change the
   semantic version.

## Alternatives considered

### A. The `exit_code → after_X` edge (chosen)

Pros:

- No schema or runtime change.
- Honest data dependency.
- Same failure propagation as the source.

Cons:

- A synthetic input per ordering edge.
- An expected validator warning.
- Noisier graphs when a source has many ordering-only dependencies.

### B. Add a control-dependency type to schema v0 (`after: [X]`)

Pros:

- Clean imported graphs.
- Useful for hand-written graphs too.

Cons:

- Amends ADR-0033 and rule 4.
- Changes the scheduler, validator, editor and hash.
- Invites ordering-only design, which the project deliberately avoids.

Revisit if imported graphs show that most dependencies are ordering-only.

### C. Run agentctl v1 directly through a compatibility layer

Pros:

- Zero migration effort.

Cons:

- Two formats in the runtime.
- Interpolation semantics (shell injection) imported as-is.
- Contradicts ADR-0033.

### D. Document a manual migration guide only

Pros:

- No code.

Cons:

- Leaves the largest switching cost in place.
- Manual rewrites tend to drop `network: false` and similar properties
  silently.

## Consequences

### Positive

- Existing graphs move in minutes, with a reviewable report.
- Imported graphs are safer than their source, because interpolated values
  are no longer executed.

### Negative / trade-offs

- Imported scripts still run through `sh -c`. Converting them to argv form is
  manual.
- `after_X` inputs and their warnings are visible noise until alternative B is
  decided.
- The importer must track the other project's format, which has no versioned
  specification. A format change there needs a new `--from` value.

## Compatibility and migration

No change to schema v0, the runtime or events. A new CLI subcommand.

## Validation

F0.1a acceptance criteria on a synthetic fixture corpus, plus one import of
real graphs by an adopting user, with the report reviewed (recorded in
`docs/validation/` without client data).
