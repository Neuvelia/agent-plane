# ADR-0039 - ReviewWarrior sends pull request content to the Anthropic API

**Status:** proposed
**Date:** 2026-11-27

## Context

ReviewWarrior (`.github/workflows/review-warrior.yml`,
`.github/review-warrior/README.md`) reviews pull requests of this repository
with a hosted model. To do that it sends repository content to a remote
service: the diff, the title and description, comments from people who can
merge, the instruction files and any file the reviewer reads from the checkout.

AGENTS.md requires an ADR for a decision that affects local-vs-cloud trust
boundaries, and its security rules forbid sending repository content to a
remote service implicitly. The product rules (local-first, no mandatory remote
service in the execution engine) apply to AgentPlane itself; this is
development tooling that runs in GitHub Actions, but it moves the same
repository's content across the same boundary, so the decision is recorded.

## Decision

1. **Explicit, repository-level opt-in.** Content is sent only when the
   `ANTHROPIC_API_KEY` repository secret is set. Without it the workflow posts
   a notice and sends nothing. Removing the secret withdraws the opt-in.
2. **Scope of what is sent.** Only pull requests from branches of this
   repository are reviewed automatically; forks and drafts only on request by
   someone with `write` or `admin` permission. What is sent is listed in the
   README's "Trust boundary" section, which is kept in step with the workflow.
3. **Outside the product.** Nothing in `cmd/`, `internal/` or `desktop/` calls
   the service or depends on the workflow; the AgentPlane runtime stays
   local-first and the review is not part of any execution path.
4. **Containment of the reviewer.** The reviewer is read-only and gets no
   GitHub token. Instructions and the gating tool come from the base commit.
   The checkout holds no symbolic links, so the file tools cannot read outside
   it (for example `/proc/self/environ`, which holds the API key). A review
   that contains the API key is not published. Model text is published with
   mentions and HTML neutralized and workflow markers stripped.
5. **Advisory until required.** The `ReviewWarrior` status gates a merge only
   when branch protection makes it required; it fails closed when a review did
   not complete, parse or publish.

## Alternatives considered

### Self-hosted or local model

Pros:

- No repository content leaves infrastructure the project controls.

Cons:

- Needs a runner with a capable model; review quality and maintenance cost are
  much worse for a single-maintainer repository.

### No automated review

Pros:

- No new trust boundary.

Cons:

- Loses a consistent first-pass review against AGENTS.md and the domain rules.

## Consequences

### Positive

- Every pull request gets a reproducible review against the base-commit rules,
  and a deterministic gate on P0/P1 findings.

### Negative / trade-offs

- Repository content, including unreleased code, is processed by the Anthropic
  API under the account's data terms.
- The reviewer process holds the API key; the defences in decision 4 reduce
  but do not remove the chance of a prompt-injected disclosure in an encoded
  form the exact-match check does not see.

## Compatibility and migration

None. No graph, event, storage or API contract changes. Disabling is removing
the secret or the workflow file.

## Validation

- `go test ./scripts/reviewwarrior` covers the review-shape gate, marker and
  mention neutralization, response selection and path handling.
- The first pull request opened after the workflow merges is the end-to-end
  check; its run log shows what was composed and sent.
