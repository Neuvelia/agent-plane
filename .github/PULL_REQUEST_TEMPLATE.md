## Feature / issue

- Feature ID (also in the PR title, `[Fx.y] ...`):
- Spec:
- Issues:

## What changed

Describe behavior, not only files.

## Acceptance criteria

Criterion IDs from `docs/roadmap/features.yaml` (for example `F1.7-AC1`) and the tagged tests that prove them.

- [ ] Fx.y-AC1 - evidence:
- [ ] Fx.y-AC2 - evidence:

## Roadmap registry

- [ ] `docs/roadmap/features.yaml` updated (criterion status/evidence, feature status, divergences, tasks).
- [ ] `make roadmap` run; `docs/roadmap/STATUS.md` committed.
- [ ] No scope or contract change without an ADR (canonical spec: `docs/roadmap/canonical-spec.md`).

## Tests and checks

```text
make check
# paste concise result
```

Additional targeted tests:

```text
...
```

## Architecture / ADR

- [ ] No durable architecture decision changed.
- [ ] ADR added/updated: `docs/adr/...`

## Scope guard

- [ ] No unrelated refactor.
- [ ] No future roadmap feature implemented implicitly.
- [ ] No secret/config material committed.

## Risk / failure cases reviewed

- [ ] Invalid input
- [ ] Runtime failure
- [ ] Cancellation/process lifecycle when applicable
- [ ] Concurrency/order when applicable
- [ ] Serialization/backward compatibility when applicable

## Known limitations

List explicit limitations or write `None beyond feature spec`.

## Reviewer notes

What should a reviewer inspect most carefully?
