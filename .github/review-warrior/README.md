# ReviewWarrior

ReviewWarrior is the automated pull request reviewer for this repository. A
model reviews the diff and posts its findings as a pull request comment; a
deterministic gate counts unresolved P0 and P1 findings and sets the
`ReviewWarrior` commit status on the head commit.

It is development tooling for this repository, not part of the AgentPlane
product. The product's local-first rules are unchanged.

## Files

| Path | Role |
| --- | --- |
| `.github/workflows/review-warrior.yml` | Triggers, authorization, GitHub API calls, the model run and the status. |
| `.github/review-warrior/prompt.md` | Reviewer instructions and the exact output shape. |
| `.github/review-warrior/rules/*.md` | Domain review rules, each applied when a changed path matches its `include` globs. |
| `scripts/reviewwarrior/` | Go tool: composes the prompt, selects author responses, checks the review shape, renders the comment. Tested by `make check`. |

The prompt, the rules, the `AGENTS.md` files and the Go tool are all taken from
the pull request's **base** commit. A pull request cannot rewrite the
instructions or the gate of its own review; a change to them takes effect on
the pull requests opened after it merges. The pull request that first adds
ReviewWarrior is therefore skipped, with a passing status.

## How a run works

1. **Trigger.** Every non-draft pull request from a branch of this repository,
   on open, push, reopen and ready-for-review. Forks and drafts are reviewed
   only on request.
2. **Authorize.** A comment that starts with `/review-warrior review` (only
   commits since the last review) or `/review-warrior full review` (the whole
   pull request) is honoured only from someone with `write` or `admin`
   permission. The command must open the comment, so a quote reply never
   starts a run. Authorized runs of one pull request run one at a time.
   GitHub keeps one queued run per pull request: a newer run that replaces a
   queued one reads its request comment as a response, but a replaced
   `full review` request becomes an incremental review; ask again if needed.
3. **Compose.** The tool builds the prompt from the base-commit instructions,
   the root `AGENTS.md` and every `AGENTS.md` above a changed file, the
   matching rules, and the untrusted pull request context: title, description,
   previous findings, the withdrawal ledger, author responses and the diff.
   Every free-text value is fenced. A diff over 200 KB is cut to whole files
   within 120 KB and the reviewer is told which files to read itself.
4. **Review.** The checkout is moved to the pull request's head commit, the
   one the diff and the status are about, and every symbolic link is removed
   from its working tree, so the file tools can only read pull request blobs.
   Claude Code runs headless and read-only: `--bare` (no
   `CLAUDE.md`, hooks or plugins from the checkout), `--restricted` (no
   settings files, file tools confined to the checkout), only `Read`, `Grep`,
   `Glob` and `git diff/show/log/ls-files`, no network tools, 15-minute limit.
   Its `git` is a shim that runs `reviewwarrior git-guard`: it refuses other
   subcommands, `--no-index`, `--output`, `-O` (also inside a bundle such as
   `-pO`), external diff helpers and any path or option value that is
   absolute, starts with `~` or climbs with `..`, because `git diff`
   with a path outside the repository silently reads that file. Deny patterns
   on the command text are a second layer.
   Model, effort and CLI version are fixed in the workflow.
5. **Check.** `reviewwarrior check` accounts for every line of the review:
   the four headings exactly, at column 0 and in order, with nothing before
   the first and no other heading or workflow marker anywhere. The verdict
   carries no `[TAG]` or `[Pn]`. A tagged section is either `None.` /
   `No actionable findings.` alone, or numbered entries at column 0 with a
   `[TAG][Pn]` prefix, each followed only by indented text with no tag in it.
   Anything else, such as a bulleted finding or one under an extra heading, is
   malformed, and so is a carriage return without a line feed, which GitHub
   reads as a line break. A malformed review is a failure, never
   "no findings". A
   review that contains the API key verbatim is a failure and is not
   published; an encoded copy is not detected.
6. **Publish.** Every `@mention` and `<` is neutralized, inside code too (a
   `<` in code shows as `&lt;`). The new
   review replaces every earlier review comment, read fresh at publish time.
   The comment records the reviewed commit and the newest comment the run
   read; the next run reads author responses from that point, inclusive, so
   a reply written during a run is not skipped (a reply from that same second
   is read twice). An edit to an older comment is not read again. A failed run posts a failure notice
   and keeps the last review, which carries the findings and the withdrawal
   ledger.
7. **Gate.** The `ReviewWarrior` status is `success` only when the review
   completed, parsed, was published and has no `[NEW]` or `[REMAINS]` finding
   at P0 or P1.

## Disputing a finding

Write a new comment that starts with `/review-warrior review` and gives the
concrete, checkable reason the finding is wrong (a line, a guard, a contract, a
test).
The reviewer withdraws the finding only after verifying that reason in the code.
Withdrawals are carried forward, so the same finding does not return on the
next push. Only comments from people who can merge are read as responses.

## Setup

1. Add the repository secret `ANTHROPIC_API_KEY`. Without it every run ends
   with a notice and no status.
2. Start in advisory mode: leave the `ReviewWarrior` status optional and watch
   the findings for a few weeks.
3. To make it a merge gate, add `ReviewWarrior` as a required status check in
   the branch protection rule for `main`.

## Trust boundary

The decision is recorded in
[ADR-0039](../../docs/adr/0039-review-warrior-sends-pull-request-content-to-anthropic.md).

When the secret is set, each run sends the pull request diff, the title,
description and authorized comments, the instruction files, and any file the
reviewer chooses to read from the checkout to the Anthropic API. This is an
explicit, repository-level opt-in made by adding the secret. Do not enable it
for a repository whose content must not leave GitHub.

The model never receives a GitHub token, cannot run code from the pull
request, cannot follow a symbolic link out of the checkout, cannot point Git
at a path outside it, and cannot reach the network except through the review
it returns. Its process does hold the API key; a review that quotes it
verbatim is not published, but an encoded copy would not be caught, so the
containment above is the defence, not the check.
The rendered comment strips any workflow marker the model writes, so a review
cannot forge the reviewed commit or the comment identity.

## Changing the reviewer

- Edit `prompt.md` or a rule; keep the output shape in `prompt.md` in step with
  `scripts/reviewwarrior/review.go`, which enforces it.
- A new rule file needs front matter with at least one `include` glob; `*`
  stays in one path segment and `**` crosses segments.
- Run `go test ./scripts/reviewwarrior` after changing the tool. To see a
  prompt locally:

  ```bash
  go run ./scripts/reviewwarrior compose -repo . -instructions . -base "$(git merge-base origin/main HEAD)" -head "$(git rev-parse HEAD)" -out /tmp/review-prompt.md
  ```
