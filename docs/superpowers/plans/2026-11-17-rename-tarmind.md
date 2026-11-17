# Rename agentplane to tarmind: preparation plan

Status: prepared, not applied. Scope: REL1 prerequisite (spec in PR #80,
`docs/features/REL1-release-engineering.md`, section "Prerequisite: rename").
This change adds tooling and this plan only; it does not rename anything.

Tooling:

- `scripts/rename-tarmind.sh` applies the rename (idempotent).
- `scripts/rename-tarmind-verify.sh` fails on any case-insensitive `agentplane`
  outside the allow-list below.

## Inventory (origin/main e55af36)

| Category | Count |
| --- | --- |
| Tracked files | 606 |
| Files that mention `agentplane` (any case) | 273 |
| Go files with a mention | 163 (409 import lines `"agentplane/..."`) |
| Files under `cmd/agentplane/` | 41 (move to `cmd/tarmind/`) |
| Mentions by top level | internal 136 files, docs 48, cmd 34, desktop 38, prompts 6, `.agents` 3, scripts 1, experiments 1, root files 6 |
| Spellings | `agentplane` 778, `AGENTPLANE` 73, `AgentPlane` 48 lines |
| Workflows (`.github/`) | 0 mentions; `ci.yml` calls `scripts/check.sh`. The release workflow arrives with REL1 and is written with the new name |
| Makefile | 2 lines (`go build -o bin/agentplane ./cmd/agentplane`, a comment) |
| Go module | `module agentplane` in `go.mod`; `.golangci.yml` import rules and `internal/archtest` (module path, `cmd/agentplane` layer root, baseline) |

Environment variables (occurrences, use sites):

| Variable | Count | Use | After |
| --- | --- | --- | --- |
| `AGENTPLANE_ATTEMPT` | 31 | injected into shell node processes (`internal/shell`), reserved name, read by example graphs and templates | `TARMIND_ATTEMPT`, plus deprecated `AGENTPLANE_ATTEMPT` for one release |
| `AGENTPLANE_BIN` | 2 | `desktop/electron/main.cjs`, `desktop/e2e/demo.cjs` | `TARMIND_BIN`; the old name is not read |
| `AGENTPLANE_STORE_DIR` | 3 | same, plus `desktop/README.md` | `TARMIND_STORE_DIR`; the old name is not read |
| `AGENTPLANE_UPDATE_FIXTURES` | 12 | test fixture regeneration | `TARMIND_UPDATE_FIXTURES` |
| `AGENTPLANE_CODEX_BIN`, `_CLI_HELPER`, `_HELPER`, `_TEST_SECRET` | 8, 7, 5, 5 | tests | renamed, no alias |

Interpretation of "AGENTPLANE_BIN and AGENTPLANE_STORE_DIR simply disappear":
the old names stop being read and no alias is set. The override concept stays
under the new names because `make desktop-e2e` and REL1 acceptance (a packaged
app honours a binary override) need it.

Desktop identifiers:

- `desktop/package.json` name `agentplane-desktop` becomes `tarmind-desktop`
  (also in `package-lock.json`); the script adds `productName: "Tarmind"` and
  `build.appId: "run.tarmind.desktop"`. Electron derives the `userData`
  directory from the product name, so it becomes `Tarmind`. There is no
  migration; before the first release, old runs stay in the previous
  `agentplane-desktop` directory under the OS application-data folder
  (`~/.config/agentplane-desktop` on Linux, `~/Library/Application Support/agentplane-desktop` on macOS).
  The runbook for the first release must say so.
- The window title and `index.html` title already read `Tarmind`.
- The preload bridge `window.agentplane` becomes `window.tarmind`; the
  localStorage keys `agentplane.locale` and `agentplane.welcome.v*` change, so
  a developer's saved locale resets once.
- Default CLI store `.agentplane/runs` becomes `.tarmind/runs`; `.gitignore`
  keeps `.agentplane/` ignored too.

## Allow-list (verifier)

Whole files that may still mention the old name:

1. `.agents/**` (example graphs and templates; decision in REL1, untouched).
2. `docs/adr/0NNN-*.md` (historical ADRs keep their wording; each file that
   mentions the old name must carry the `> Rename note:` line the script adds).
3. This plan, `docs/features/REL1-release-engineering.md`, and the two rename
   scripts.
4. `desktop/test/fixtures/graph-*-demo.json` and `exhausted.yaml`: recordings of
   `.agents/` graphs, which must stay byte-identical to their sources.

Single lines that may still mention it:

5. `refs/agentplane` anywhere. These are persisted workspace ref names
   (`internal/worktree`, `internal/domain/events/workspace.go`, event payloads
   and fixtures). They are durable event data; renaming them would invalidate
   stored runs. Decision for the owner: keep (default here) or schedule a
   separate migration.
6. The deprecated alias: `AGENTPLANE_ATTEMPT` (any case) in
   `internal/shell/executor.go`, `internal/shell/attempt_alias_test.go`,
   `internal/fields/fieldstest/fieldstest.go`, `cmd/tarmind/templates_test.go`,
   and the "Deprecated alias" section in `docs/features/F1.6a-shell-loop.md`.
7. `.gitignore`: the line `.agentplane/`.
8. Any line beginning `> Rename note:`.

JSON and event field names: there are none prefixed with `agentplane` in the
code today (the string `"agentplane"` hits are all import paths), so the
allow-list needs no entry; `schema_version` is not touched. The verifier also
searches for the obfuscated spellings `[a]gentplane` (used by the e2e `pkill`
pattern) and `agent<sep>plane`; the script rewrites the first.

## The one logic change: AGENTPLANE_ATTEMPT dual name

Applied by step 5 of the script on `internal/shell/executor.go`:

- `AttemptEnv = "TARMIND_ATTEMPT"`; new `DeprecatedAttemptEnv = "AGENTPLANE_ATTEMPT"`.
- For a loop attempt (`attempt > 0`) the executor appends both
  `TARMIND_ATTEMPT=<n>` and `AGENTPLANE_ATTEMPT=<n>` (same value).
- Both names are reserved in any letter case in `env`, `env_allowlist` and
  `input_env` (the existing `reserved` check also matches the deprecated name).
- The `.agents/` templates and example graphs keep reading `$AGENTPLANE_ATTEMPT`
  and still work because of the alias. The templates test allows both names.
- Removal: a later change deletes `DeprecatedAttemptEnv`, its test file and
  the doc section, and updates `.agents/` in a separate decision.

Test, `internal/shell/attempt_alias_test.go` (added by the script):

- `TestDeprecatedAttemptVariableMatchesTheCurrentOne`: for attempts 1 and 3 a
  child running `printenv` of each name sees the same number; with attempt 0
  neither is injected.
- `TestDeprecatedAttemptVariableIsReservedInAnyCase`: `AGENTPLANE_ATTEMPT`,
  `agentplane_attempt`, `AgentPlane_Attempt` in `env` and `env_allowlist` are
  rejected with a "reserved" error.

The existing tests, renamed to `TARMIND_ATTEMPT`, keep covering the new name.

## Order of operations

1. Agree a freeze: merge or rebase the open stacks (F3.1, refactor, REL1 docs)
   first, so the rename lands on a quiet `main`.
2. Branch from `origin/main`; require a clean tree.
3. `scripts/rename-tarmind.sh` (moves `cmd/agentplane`, `go mod edit -module`,
   import literals plus `gofmt` import sorting, allow-listed text rewrite, the
   alias change, desktop identity, ADR notes).
4. `scripts/rename-tarmind-verify.sh`.
5. `make check`, `cd desktop && npm ci && npm run check`, `make desktop-e2e`.
6. Review `git diff -M --stat`: renames only plus the files named in
   "The one logic change". Commit as one mechanical PR, separate from REL1.
7. Rename the GitHub repository after merge (REL1 open question 2).

## How open branches rebase

Rebasing a stack across a rename conflicts in nearly every file. Use the same
script instead of resolving by hand:

1. Rebase the branch onto the last pre-rename `main` commit, so it builds there.
2. On the branch tip run `scripts/rename-tarmind.sh` and commit the result as
   one "rename" commit (the script is idempotent and also renames files the
   branch added).
3. Rebase onto the post-rename `main`:
   `git rebase --onto <rename-merge-commit> <pre-rename-main> <branch>`.
   Resolve conflicts in the branch's own commits by taking the branch side and
   re-running the script; then drop the temporary "rename" commit, because
   `main` already contains the rename.
4. Run `scripts/rename-tarmind-verify.sh` and `make check` on the result.

Branches with large diffs can instead be replayed: `git rebase --exec
'scripts/rename-tarmind.sh && git commit -a --amend --no-edit'` over their
commits, which renames each commit before it is applied on the new `main`.

## Verification

```bash
scripts/rename-tarmind-verify.sh
make check
(cd desktop && npm ci && npm run check)
make desktop-e2e
go test -race ./internal/shell/...
```

Golden CLI tests (`cmd/tarmind/testdata/golden/*`) differ from the old files
only by the command name: `sed s/agentplane/tarmind/g` over the old goldens
equals the new ones (0 residual lines in all 5).

## Dry run on origin/main e55af36 (throwaway worktree, not committed)

- Script: imports rewritten in 151 Go files, 126 text files rewritten,
  41 files moved, 10 ADRs noted, 270 files changed. A second run changes
  nothing (diff identical).
- Verifier: OK, 37 files keep an allowed mention.
- `make check`: OK. `npm run check` in `desktop/`: OK. `make desktop-e2e`: OK
  after working around a defect that exists on `main` (below).

Found along the way: `desktop/e2e/demo.cjs` line 221 copies
`internal/graph/edit/testdata/hostile.yaml`, which moved to
`internal/adapter/yamlgraph/edit/testdata/hostile.yaml` in the refactor, so
`make desktop-e2e` fails on current `main` before the rename. Fix it first, in
its own change.

## Risks

- Text rewrite is by file allow-list, so a new top-level directory needs an
  entry in `INCLUDE`; the verifier catches the omission.
- The `pkill` pattern in the e2e uses the `[a]gentplane` trick; plain case-
  insensitive grep and sed miss it (the script and verifier handle it).
- Prompts and docs outside `docs/adr` are rewritten; their history stays in Git.
