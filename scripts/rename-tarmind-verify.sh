#!/usr/bin/env bash
# Fails on any case-insensitive "agentplane" outside the explicit allow-list.
# Run after scripts/rename-tarmind.sh. Allow-list: docs/superpowers/plans/2026-11-17-rename-tarmind.md
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

# Whole files that may mention the old name (path regex, ERE).
ALLOW_PATHS=(
  '^\.agents/'                                       # example graphs, untouched by decision
  '^docs/adr/0[0-9]{3}-.*\.md$'                      # historical ADRs (each must carry the rename note)
  '^docs/superpowers/plans/2026-11-17-rename-tarmind\.md$'
  '^docs/features/REL1-release-engineering\.md$'     # specifies the rename
  '^scripts/rename-tarmind(-verify)?\.sh$'
  '^desktop/test/fixtures/graph-.*-demo\.json$'      # recordings of .agents/ graphs
  '^desktop/test/fixtures/exhausted\.yaml$'
)
# Individual lines: "path regex<TAB>line regex". A line must match both.
ALLOW_LINES=(
  # persisted event data and git refs: durable contract, not renamed
  $'.*\trefs/agentplane'
  # the deprecated AGENTPLANE_ATTEMPT alias (code, tests, docs)
  $'^(internal/shell/executor\\.go|internal/shell/attempt_alias_test\\.go|internal/fields/fieldstest/fieldstest\\.go|cmd/tarmind/templates_test\\.go)$\t(?i:agentplane_attempt)'
  $'^docs/features/F1\\.6a-shell-loop\\.md$\tAGENTPLANE_ATTEMPT'
  # old local run store stays ignored
  $'^\\.gitignore$\t^\\.agentplane/$'
  # rename note lines
  $'.*\t^> Rename note:'
)

pcre() { grep -qP "$1" <<<"$2"; }

fail=0
declare -A seen
while IFS= read -r hit; do
  path="${hit%%:*}"; rest="${hit#*:}"; text="${rest#*:}"
  ok=0
  for re in "${ALLOW_PATHS[@]}"; do
    if grep -qE "$re" <<<"$path"; then ok=1; break; fi
  done
  if [ "$ok" = 0 ]; then
    for entry in "${ALLOW_LINES[@]}"; do
      pathre="${entry%%$'\t'*}"; linere="${entry#*$'\t'}"
      if grep -qE "$pathre" <<<"$path" && pcre "$linere" "$text"; then ok=1; break; fi
    done
  fi
  if [ "$ok" = 0 ]; then
    printf 'DISALLOWED: %s\n' "$hit"
    fail=$((fail + 1))
  fi
  seen[$path]=1
done < <(git grep -n -I -i --untracked -e agentplane -e '\[a\]gentplane' -e 'agent[^a-z]\{0,2\}plane' || true)

# Every historical ADR that still names the old product must carry the note.
while IFS= read -r adr; do
  grep -q '^> Rename note:' "$adr" || { echo "MISSING rename note: $adr"; fail=$((fail + 1)); }
done < <(git grep -l -I -i --untracked -e agentplane -- 'docs/adr/0[0-9][0-9][0-9]-*.md' || true)

# Structural checks that grep for the old name cannot see.
[ ! -e cmd/agentplane ] || { echo "cmd/agentplane still exists"; fail=$((fail + 1)); }
[ -d cmd/tarmind ] || { echo "cmd/tarmind is missing"; fail=$((fail + 1)); }
[ "$(go list -m 2>/dev/null || true)" = "tarmind" ] || { echo "go.mod module is not tarmind"; fail=$((fail + 1)); }

if [ "$fail" -ne 0 ]; then
  echo "rename verification FAILED: $fail finding(s)" >&2
  exit 1
fi
echo "rename verification OK: ${#seen[@]} files keep an allowed mention"
