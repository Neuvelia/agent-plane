#!/usr/bin/env bash
# Mechanical rename agentplane -> tarmind (REL1 prerequisite).
# Idempotent: a second run changes nothing. Run from anywhere inside the repo,
# on a clean branch, then run scripts/rename-tarmind-verify.sh and `make check`.
# Plan and allow-list: docs/superpowers/plans/2026-11-17-rename-tarmind.md
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

command -v go >/dev/null || { echo "go not found in PATH" >&2; exit 2; }
command -v perl >/dev/null || { echo "perl not found in PATH" >&2; exit 2; }

# Text files rewritten by the allow-listed sed pass. Everything else is left alone.
INCLUDE=(
  'Makefile' 'README.md' 'AGENTS.md' '.gitignore' '.golangci.yml'
  'cmd/*' 'internal/*' 'experiments/*' 'scripts/*' 'prompts/*' 'docs/*' '.github/*' 'desktop/*'
)
# Never rewritten by sed (the verifier allow-list mirrors this list).
EXCLUDE=(
  ':!.agents/**'                                   # example graphs: durable, untouched
  ':!docs/adr/0[0-9][0-9][0-9]-*.md'               # historical ADRs: note line only
  ':!docs/superpowers/plans/2026-11-17-rename-tarmind.md'
  ':!docs/features/REL1-release-engineering.md'    # describes the rename itself
  ':!scripts/rename-tarmind.sh' ':!scripts/rename-tarmind-verify.sh'
  ':!desktop/node_modules/**' ':!desktop/dist/**' ':!desktop/storybook-static/**'
  # recordings of .agents/ graphs: must stay byte-identical to the example files
  ':!desktop/test/fixtures/graph-*-demo.json'
  ':!desktop/test/fixtures/exhausted.yaml'
)

changed=0
note() { printf '%s\n' "$*"; }

# 1. Directory move.
if [ -d cmd/agentplane ]; then
  git mv cmd/agentplane cmd/tarmind
  note "moved cmd/agentplane -> cmd/tarmind"
fi

# 2. Go module path.
if [ "$(go list -m 2>/dev/null)" != "tarmind" ]; then
  go mod edit -module tarmind
  note "go.mod module -> tarmind"
fi

# 3. Go import paths: rewrite only import-spec string literals that start with
#    the old module path, then let gofmt re-sort the import blocks.
mapfile -t gofiles < <(git ls-files -z '*.go' | xargs -0 grep -lE '^[[:space:]]*([A-Za-z_.]+[[:space:]]+)?"agentplane(/|")' || true)
if [ "${#gofiles[@]}" -gt 0 ]; then
  perl -0pi -e 's/^(\s*(?:[A-Za-z_.]+\s+)?)"agentplane(\/|")/$1"tarmind$2/mg' "${gofiles[@]}"
  gofmt -w "${gofiles[@]}"
  note "rewrote imports in ${#gofiles[@]} Go files"
fi

# 4. Allow-listed text rewrite. Shielded: refs/agentplane (persisted event data,
#    durable contract). AGENTPLANE_ATTEMPT becomes TARMIND_ATTEMPT here; step 5
#    re-adds the deprecated name deliberately.
mapfile -t files < <(git ls-files -z -- "${INCLUDE[@]}" "${EXCLUDE[@]}" | xargs -0 grep -IlsiE 'agentplane' || true)
# Once step 5 has run, these files deliberately keep the deprecated name.
if grep -q 'DeprecatedAttemptEnv' internal/shell/executor.go 2>/dev/null; then
  ALIAS_FILES='internal/shell/executor.go internal/shell/attempt_alias_test.go internal/fields/fieldstest/fieldstest.go cmd/tarmind/templates_test.go docs/features/F1.6a-shell-loop.md'
  mapfile -t files < <(printf '%s\n' "${files[@]:-}" | grep -vxF -f <(printf '%s\n' $ALIAS_FILES) || true)
fi
[ -z "${RENAME_VERBOSE:-}" ] || printf 'text rewrite: %s\n' "${files[@]:-}"
if [ "${#files[@]}" -gt 0 ]; then
  perl -pi -e '
    s{refs/agentplane}{\@\@REFS\@\@}g;
    s/\[a\]gentplane/[t]armind/g;
    s/AGENTPLANE_/TARMIND_/g;
    s/AgentPlane/Tarmind/g;
    s/agentplane/tarmind/g;
    s{\@\@REFS\@\@}{refs/agentplane}g;
  ' "${files[@]}"
  note "rewrote text in ${#files[@]} files"
fi
mapfile -t gofiles2 < <(printf '%s\n' "${files[@]:-}" | grep '\.go$' || true)
[ "${#gofiles2[@]}" -gt 0 ] && gofmt -w "${gofiles2[@]}"

# 5. AGENTPLANE_ATTEMPT dual name for one release (the only logic change).
EXEC=internal/shell/executor.go
if ! grep -q 'DeprecatedAttemptEnv' "$EXEC"; then
  perl -0pi -e '
    s{(const AttemptEnv = "TARMIND_ATTEMPT"\n)}{$1\n// DeprecatedAttemptEnv carries the same value as AttemptEnv for one release so\n// existing scripts keep working. Remove it together with its tests.\nconst DeprecatedAttemptEnv = "AGENTPLANE_ATTEMPT"\n} or die "anchor: AttemptEnv const\n";
    s{(\t\tcmd\.Env = append\(cmd\.Env, AttemptEnv\+"="\+strconv\.Itoa\(attempt\)\))}{$1\n\t\tcmd.Env = append(cmd.Env, DeprecatedAttemptEnv+"="+strconv.Itoa(attempt))} or die "anchor: injection\n";
    s{return strings\.EqualFold\(name, AttemptEnv\) \}}{return strings.EqualFold(name, AttemptEnv) || strings.EqualFold(name, DeprecatedAttemptEnv) \}} or die "anchor: reserved\n";
  ' "$EXEC"
  perl -pi -e 's/(is reserved for the runtime", AttemptEnv)\)/$1)/' "$EXEC"
  note "added DeprecatedAttemptEnv to $EXEC"
fi

TT=cmd/tarmind/templates_test.go
if ! grep -q 'AGENTPLANE_ATTEMPT' "$TT"; then
  perl -pi -e 's/allowed := map\[string\]bool\{"TARMIND_ATTEMPT": true\}/allowed := map[string]bool{"TARMIND_ATTEMPT": true, "AGENTPLANE_ATTEMPT": true}/' "$TT"
  grep -q 'AGENTPLANE_ATTEMPT' "$TT" || { echo "anchor missing in $TT" >&2; exit 3; }
fi

FT=internal/fields/fieldstest/fieldstest.go
if ! grep -q '"agentplane_attempt"' "$FT"; then
  perl -pi -e 's/"tarmind_attempt"\}/"tarmind_attempt", "agentplane_attempt"}/' "$FT"
  grep -q '"agentplane_attempt"' "$FT" || { echo "anchor missing in $FT" >&2; exit 3; }
fi

ALIAS_TEST=internal/shell/attempt_alias_test.go
if [ ! -f "$ALIAS_TEST" ]; then
  cat > "$ALIAS_TEST" <<'EOF'
package shell

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"tarmind/internal/domain/graph"
	"tarmind/internal/testenv"
)

func attemptNodeFor(variable string, extra map[string]any) graph.NodeDefinition {
	cfg := map[string]any{"command": "/usr/bin/printenv", "args": []any{variable}}
	for k, v := range extra {
		cfg[k] = v
	}
	return graph.NodeDefinition{Type: "shell", Config: cfg, Outputs: map[string]graph.PortDefinition{"stdout": {Type: graph.ValueTypeString}}}
}

// The deprecated name carries the same value as the current one (one release).
func TestDeprecatedAttemptVariableMatchesTheCurrentOne(t *testing.T) {
	testenv.RequireExecutable(t, "/usr/bin/printenv")
	for _, attempt := range []int{1, 3} {
		for _, name := range []string{AttemptEnv, DeprecatedAttemptEnv} {
			out, err := Executor{}.ExecuteNodeAttempt(context.Background(), "n", attemptNodeFor(name, nil), nil, nil, attempt)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got, _ := out["stdout"].AsString(); got != strconv.Itoa(attempt)+"\n" {
				t.Fatalf("%s attempt %d: child saw %q", name, attempt, got)
			}
		}
	}
	if _, err := (Executor{}).ExecuteNodeAttempt(context.Background(), "n", attemptNodeFor(DeprecatedAttemptEnv, nil), nil, nil, 0); err == nil {
		t.Fatal("a non-loop execution must not receive the deprecated variable either")
	}
}

func TestDeprecatedAttemptVariableIsReservedInAnyCase(t *testing.T) {
	for _, name := range []string{"AGENTPLANE_ATTEMPT", "agentplane_attempt", "AgentPlane_Attempt"} {
		for field, extra := range map[string]map[string]any{
			"env":           {"env": map[string]any{name: "9"}},
			"env_allowlist": {"env_allowlist": []any{name}},
		} {
			err := Executor{}.ValidateNodeConfig("n", attemptNodeFor(AttemptEnv, extra))
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("%s %s: want a 'reserved' error, got %v", field, name, err)
			}
		}
	}
}
EOF
  note "added $ALIAS_TEST"
fi

gofmt -w "$EXEC" "$TT" "$FT" "$ALIAS_TEST"

# 6. Deprecation note for the old variable, once, in the graph spec's neighbour doc.
DOC=docs/features/F1.6a-shell-loop.md
if [ -f "$DOC" ] && ! grep -q 'Deprecated alias' "$DOC"; then
  printf '\n## Deprecated alias\n\n`AGENTPLANE_ATTEMPT` is still set to the same value as `TARMIND_ATTEMPT` for one release after the rename to Tarmind. It is deprecated: read `TARMIND_ATTEMPT`. Both names stay reserved in any letter case.\n' >> "$DOC"
  note "documented the deprecated alias in $DOC"
fi

# 7. Desktop identity: product name (drives the userData directory name) and bundle id.
if [ -f desktop/package.json ] && command -v node >/dev/null; then
  node -e '
    const fs = require("fs"), p = "desktop/package.json"
    const j = JSON.parse(fs.readFileSync(p, "utf8")), before = JSON.stringify(j)
    if (j.productName !== "Tarmind") {
      const out = {}
      for (const [k, v] of Object.entries(j)) { out[k] = v; if (k === "name") out.productName = "Tarmind" }
      Object.keys(j).forEach((k) => delete j[k]); Object.assign(j, out)
    }
    j.build = { ...(j.build || {}), appId: "run.tarmind.desktop", productName: "Tarmind" }
    if (JSON.stringify(j) !== before) fs.writeFileSync(p, JSON.stringify(j, null, 2) + "\n")
  '
fi

# 8. Historical ADRs keep their wording; add one note line under the title.
NOTE='> Rename note: the product was renamed from AgentPlane to Tarmind (command `tarmind`, module `tarmind`); this record keeps its original wording.'
notes=0
while IFS= read -r adr; do
  grep -q '^> Rename note:' "$adr" && continue
  perl -0pi -e 'BEGIN{$n=shift} s/\A([^\n]*\n)/$1\n$n\n/' "$NOTE" "$adr"
  notes=$((notes + 1))
done < <(git ls-files 'docs/adr/0[0-9][0-9][0-9]-*.md' | xargs grep -lsi 'agentplane' || true)
note "rename note added to $notes ADRs"

echo
echo "summary:"
git status --porcelain | awk '{c[substr($0,1,2)]++} END{for(k in c) printf "  %s %d\n", k, c[k]}'
echo "next: scripts/rename-tarmind-verify.sh && make check"
