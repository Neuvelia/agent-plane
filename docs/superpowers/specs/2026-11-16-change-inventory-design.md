# H19: Read-only change inventory for developer verification

## Intent

Selective verification needs an accurate view of local changes before it can
choose tests. The current repository has full `make check` and desktop gates,
but no command that inventories staged, unstaged and untracked files together.
Add a small read-only command that can be checked independently of future test
selection or execution.

## Approaches

- **Go command using Git's NUL-delimited output (chosen):** standard-library
  process execution and JSON encoding, with integration tests in temporary Git
  repositories. It fits the existing Go toolchain and can later feed a planner.
- **Shell pipeline:** fewer lines, but safely combining rename paths, unusual
  filenames and multiple change sources is fragile across shells.
- **Parse `git status --porcelain`:** one Git call, but rename/copy records and
  staged versus working-tree status require more protocol-specific parsing.

## Scope and behavior

- Add `make verify-changes`, invoking `go run ./scripts/verifychanges` from
  the repository root. The built command also accepts a subdirectory as its
  working directory.
- Discover the repository root through Git, then combine three read-only,
  NUL-delimited queries: staged diff, unstaged diff and untracked non-ignored
  files. Use `--no-renames` so both old and new paths of a rename appear.
  Disable optional Git locks, filesystem monitor hooks and external diff tools
  for the inventory queries.
- Report each repository-relative path once with sorted sources from
  `staged`, `unstaged`, `untracked`. Sort records by path. Preserve spaces and
  newlines in filenames through JSON escaping; reject invalid UTF-8 paths
  rather than silently replacing bytes.
- Output one JSON document with `checks_executed: false` and a `changes` list.
  An empty list is valid. Exit zero means inventory succeeded, not that any
  tests passed. Git errors produce a nonzero exit and no success-shaped JSON.
- Do not print or persist file contents, run tests, choose commands, write Git
  state or add a product runtime dependency. The JSON is a local developer-tool
  output; no product graph, event, API or CLI contract changes.

## Acceptance

- A temp repository test proves staged, unstaged and untracked changes are all
  reported, including both sources when one file has staged and unstaged edits.
- A staged change that the working tree reverts to `HEAD` remains visible as
  staged; a staged rename reports both old and new paths.
- Running from a subdirectory still yields repository-relative paths; ignored
  files are absent; filenames with spaces or newlines round-trip in JSON.
- Outside a Git repository, the command fails and emits no JSON result.
- Focused tests and `make check` pass; the full diff contains only developer
  harness files and docs.

## Risks and limits

Combining only a single `git diff HEAD` would miss a staged change that has
been reverted in the working tree, so stage and worktree queries stay separate.
This inventory does not identify transitive Go dependencies or claim that
verification is green. A later step can select and run checks from this data.
