#!/usr/bin/env bash
# F3.1 / ADR-0024 acceptance spike: can an installed Codex CLI work inside a
# linked Git worktree whose gitdir lives in the source repository's .git?
#
# Manual, authorized, never run by CI. It needs the host's Codex authentication
# and network access. It uses only a disposable repository under a fresh
# temporary directory and never touches the AgentPlane checkout.
#
# Codex is started with the SAME argv, sandbox and explicit environment as
# internal/agent/codex/adapter.go. It does not use AgentPlane code, so the spike
# can run before F3.1 exists. Recorded evidence: verdicts, exit codes and hashes.
# Raw Codex output stays in $SPIKE_DIR/private (mode 0700); do not commit or paste it.
#
# Usage:
#   CODEX_BIN=/absolute/path/to/codex scripts/spike-f3-1-codex-worktree.sh
#
# Optional environment:
#   SPIKE_DIR      fresh/empty directory to use (default: mktemp -d)
#   SPIKE_TIMEOUT  seconds allowed per Codex run (default 900)
#   CODEX_HOME     forwarded to Codex only when set and present

set -euo pipefail

die() { printf 'spike: %s\n' "$*" >&2; exit 2; }

[ "${CODEX_BIN:-}" ] || die "set CODEX_BIN to the absolute path of the installed Codex CLI"
case "$CODEX_BIN" in /*) ;; *) die "CODEX_BIN must be an absolute path" ;; esac
[ -x "$CODEX_BIN" ] && [ -f "$CODEX_BIN" ] || die "CODEX_BIN is not an executable file"
GIT=/usr/bin/git
[ -x "$GIT" ] || die "$GIT not found (ADR-0024 uses /usr/bin/git only)"
TIMEOUT="${SPIKE_TIMEOUT:-900}"

if [ -n "${SPIKE_DIR:-}" ]; then
  mkdir -p "$SPIKE_DIR"
  [ -z "$(ls -A "$SPIKE_DIR")" ] || die "SPIKE_DIR must be empty"
  DEMO="$(cd "$SPIKE_DIR" && pwd -P)"
else
  DEMO="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/agentplane-f31-spike.XXXXXX")" && pwd -P)"
fi
chmod 700 "$DEMO"
SRC="$DEMO/src"
WT="$DEMO/worktrees"
PRIV="$DEMO/private"
mkdir -p "$SRC" "$WT" "$PRIV"
chmod 700 "$WT" "$PRIV"
RESULTS="$DEMO/results.tsv"
: > "$RESULTS"
FAILS=0

# Same controlled Git invocation as internal/gitstate (ADR-0024 section 7).
hgit() {
  env -i PATH=/usr/bin:/bin LC_ALL=C GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null \
    GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 \
    "$GIT" -c core.hooksPath=/dev/null -c core.fsmonitor=false \
    -c user.name=AgentPlaneSpike -c user.email=spike@example.invalid "$@"
}
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum; else shasum -a 256; fi; }

record() { # status name detail
  printf '%s\t%s\t%s\n' "$1" "$2" "$3" >> "$RESULTS"
  printf '%-5s %-34s %s\n' "$1" "$2" "$3"
  if [ "$1" = FAIL ]; then FAILS=$((FAILS + 1)); fi
}

# Hash of everything in the SOURCE checkout except .git (worktree metadata
# legitimately changes .git): HEAD, status, and every file's content.
fingerprint() {
  (
    cd "$SRC"
    {
      hgit rev-parse HEAD
      hgit status --porcelain=v1 --untracked-files=all
      find . -path ./.git -prune -o -type f -print | LC_ALL=C sort |
        while IFS= read -r f; do printf '%s ' "$f"; sha < "$f"; done
    } | sha
  )
}

# ---------------------------------------------------------------- preflight
echo "== preflight"
CODEX_ENV=(HOME="${HOME:?HOME must be set}" PATH="$PATH")
if [ -n "${CODEX_HOME:-}" ] && [ -e "$CODEX_HOME" ]; then CODEX_ENV+=(CODEX_HOME="$CODEX_HOME"); fi
codex() { env -i "${CODEX_ENV[@]}" "$CODEX_BIN" "$@"; }

record INFO codex_version "$(codex --version 2>/dev/null | head -1 || echo unknown)"
record INFO git_version "$(hgit --version)"
GITV="$(hgit --version | sed 's/[^0-9.]*\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/')"
set -- $GITV
if [ "${1:-0}" -gt 2 ] || { [ "${1:-0}" -eq 2 ] && [ "${2:-0}" -ge 38 ]; }; then
  record PASS git_min_version "2.38 or newer (merge-tree --write-tree)"
else
  record FAIL git_min_version "Git 2.38+ is required"
fi

HELP_GLOBAL="$(codex --help 2>&1 || true)"
HELP_EXEC="$(codex exec --help 2>&1 || true)"
MISSING=""
for f in --no-daemon --ask-for-approval; do case "$HELP_GLOBAL" in *"$f"*) ;; *) MISSING="$MISSING $f" ;; esac; done
for f in --sandbox --ignore-user-config --ephemeral --json --color --cd --output-schema --output-last-message; do
  case "$HELP_EXEC" in *"$f"*) ;; *) MISSING="$MISSING $f" ;; esac
done
if [ -z "$MISSING" ]; then record PASS codex_flags "all flags used by the AgentPlane adapter exist"
else record FAIL codex_flags "missing:$MISSING"; fi

# ------------------------------------------------------------ disposable repo
echo "== repository"
hgit -C "$SRC" init -q
mkdir -p "$SRC/sub"
printf 'before\n' > "$SRC/a.txt"
printf 'before\n' > "$SRC/b.txt"
printf 'base\n' > "$SRC/shared.txt"
printf 'base\n' > "$SRC/sub/c.txt"
hgit -C "$SRC" add .
hgit -C "$SRC" commit -q -m baseline
BASE="$(hgit -C "$SRC" rev-parse HEAD)"
FP0="$(fingerprint)"

mkwt() { # name -> prints path
  hgit -C "$SRC" worktree add -q --detach "$WT/agentplane-spike-$1" "$BASE"
  printf '%s\n' "$WT/agentplane-spike-$1"
}
W1="$(mkwt one)"
TOP="$(hgit -C "$W1" rev-parse --show-toplevel)"
GDIR="$(hgit -C "$W1" rev-parse --absolute-git-dir)"
if [ "$(cd "$TOP" && pwd -P)" = "$(cd "$W1" && pwd -P)" ] && [ -f "$W1/.git" ] && [ -d "$GDIR" ]; then
  record PASS worktree_layout ".git is a file; gitdir lives under the source .git"
else
  record FAIL worktree_layout "unexpected worktree layout"
fi

# ------------------------------------------------------------- Codex runner
cat > "$PRIV/schema.json" <<'EOF'
{"type":"object","additionalProperties":false,"required":["summary","artifacts"],"properties":{"summary":{"type":"string"},"artifacts":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["ref","name"],"properties":{"ref":{"type":"string"},"name":{"type":"string"}}}}}}
EOF

# run_codex WORKTREE TASKFILE PREFIX
# Same argv as internal/agent/codex/adapter.go. Writes PREFIX.{stdout,stderr,exit,t0,t1}.
# Meant to be started in the background so two runs can overlap.
run_codex() {
  local wt="$1" task="$2" out="$3" pid wd rc
  date +%s > "$out.t0"
  env -i "${CODEX_ENV[@]}" "$CODEX_BIN" --no-daemon --ask-for-approval never exec \
    --sandbox workspace-write --ignore-user-config --ephemeral --json --color never \
    --cd "$wt" --output-schema "$PRIV/schema.json" --output-last-message "$out.result.json" - \
    < "$task" > "$out.stdout" 2> "$out.stderr" &
  pid=$!
  ( sleep "$TIMEOUT"; kill -TERM "$pid" 2>/dev/null; sleep 5; kill -KILL "$pid" 2>/dev/null ) 2>/dev/null &
  wd=$!
  disown "$wd" 2>/dev/null || true
  set +e; wait "$pid"; rc=$?; set -e
  { pkill -P "$wd"; kill "$wd"; } >/dev/null 2>&1 || true
  date +%s > "$out.t1"
  printf '%s\n' "$rc" > "$out.exit"
}
exit_of() { cat "$1.exit" 2>/dev/null || echo missing; }

FINAL='Finish with a one-sentence summary and an empty artifacts list.'

# -------------------------------------------------- T1: edit inside worktree
echo "== T1 edit in worktree (Codex runs; this can take a minute)"
cat > "$PRIV/t1.txt" <<EOF
Replace the entire content of a.txt with exactly one line: after
Do not modify, create or delete any other file. $FINAL
EOF
run_codex "$W1" "$PRIV/t1.txt" "$PRIV/t1"
T1X="$(exit_of "$PRIV/t1")"
if [ "$T1X" = 0 ]; then record PASS t1_codex_exit "0"; else record FAIL t1_codex_exit "exit $T1X (inspect $PRIV/t1.stderr locally; do not paste it)"; fi
if printf 'after\n' | cmp -s - "$W1/a.txt"; then record PASS t1_file_content "a.txt == after"; else record FAIL t1_file_content "a.txt differs"; fi
S1="$(hgit -C "$W1" status --porcelain=v1 --untracked-files=all)"
if [ "$S1" = " M a.txt" ]; then record PASS t1_worktree_status "only ' M a.txt'"; else record FAIL t1_worktree_status "unexpected: $(printf '%s' "$S1" | tr '\n' ';')"; fi

# ------------------------------------ T2: Codex's own git calls in the worktree
echo "== T2 git inside the Codex sandbox"
cat > "$PRIV/t2.txt" <<EOF
Run these three shell commands from the repository root, one at a time, and note each exit code:
1. git status --porcelain
2. git diff --name-only
3. git add -A
Then create the file git-probe.txt in the repository root with exactly these three lines and nothing else:
status_exit=<exit code of command 1>
diff_exit=<exit code of command 2>
add_exit=<exit code of command 3>
Do not run any other git command. Do not commit. $FINAL
EOF
run_codex "$W1" "$PRIV/t2.txt" "$PRIV/t2"
T2X="$(exit_of "$PRIV/t2")"
if [ "$T2X" = 0 ]; then record PASS t2_codex_exit "0"; else record FAIL t2_codex_exit "exit $T2X"; fi
probe() { sed -n "s/^$1=//p" "$W1/git-probe.txt" 2>/dev/null | head -1; }
if [ -f "$W1/git-probe.txt" ]; then
  [ "$(probe status_exit)" = 0 ] && record PASS t2_git_status_in_sandbox "git status exit 0" \
    || record FAIL t2_git_status_in_sandbox "exit '$(probe status_exit)'"
  [ "$(probe diff_exit)" = 0 ] && record PASS t2_git_diff_in_sandbox "git diff exit 0" \
    || record FAIL t2_git_diff_in_sandbox "exit '$(probe diff_exit)'"
  # Writing the index lives in the source .git: informational, see the doc's decision table.
  record INFO t2_git_add_in_sandbox "git add exit '$(probe add_exit)'"
  CACHED="$(hgit -C "$W1" diff --cached --name-only | tr '\n' ';')"
  record INFO t2_index_after_add "staged: ${CACHED:-none}"
else
  record FAIL t2_probe_file "git-probe.txt was not created"
fi
if hgit -C "$W1" status --porcelain=v1 >/dev/null 2>&1; then record PASS t2_host_git_still_works "host git status ok after Codex"; else record FAIL t2_host_git_still_works "host git status failed"; fi
if [ -e "$GDIR/index.lock" ]; then record FAIL t2_no_stale_lock "index.lock left in worktree gitdir"; else record PASS t2_no_stale_lock "no index.lock"; fi
FP1="$(fingerprint)"
if [ "$FP1" = "$FP0" ]; then record PASS source_unchanged_after_t1_t2 "source fingerprint identical"; else record FAIL source_unchanged_after_t1_t2 "source checkout changed"; fi

# -------------------------------------------------- T3: write outside the worktree
echo "== T3 absolute-path write to the source checkout (informational)"
cat > "$PRIV/t3.txt" <<EOF
Using one shell command, try to create the file $SRC/escape-probe.txt containing the text x.
Do not retry with any other method. Then create escape-result.txt in the repository root containing exactly one line:
escape_exit=<exit code of that command>
$FINAL
EOF
run_codex "$W1" "$PRIV/t3.txt" "$PRIV/t3"
if [ -e "$SRC/escape-probe.txt" ]; then
  record INFO t3_outside_write "WRITTEN: sandbox did not block a write to the source checkout (source guard is essential)"
  rm -f "$SRC/escape-probe.txt"
else
  record INFO t3_outside_write "blocked: workspace-write sandbox refused a write outside the worktree ($(tr -d '\n' < "$W1/escape-result.txt" 2>/dev/null || echo "no result file"); codex exit $(exit_of "$PRIV/t3"))"
fi

# ------------------------------------ T4: two parallel worktrees, same file
echo "== T4 two concurrent Codex runs editing the same file"
WA="$(mkwt a)"
WB="$(mkwt b)"
for u in a b; do
  cat > "$PRIV/t4$u.txt" <<EOF
Replace the entire content of shared.txt with exactly one line: from-$u
Do not modify, create or delete any other file. $FINAL
EOF
done
run_codex "$WA" "$PRIV/t4a.txt" "$PRIV/t4a" &
PA=$!
run_codex "$WB" "$PRIV/t4b.txt" "$PRIV/t4b" &
PB=$!
wait "$PA" "$PB"
for u in a b; do
  X="$(exit_of "$PRIV/t4$u")"
  if [ "$X" = 0 ]; then record PASS "t4${u}_codex_exit" "0"; else record FAIL "t4${u}_codex_exit" "exit $X"; fi
done
if printf 'from-a\n' | cmp -s - "$WA/shared.txt" && printf 'from-b\n' | cmp -s - "$WB/shared.txt"; then
  record PASS t4_same_file_isolated "each worktree holds only its own shared.txt"
else
  record FAIL t4_same_file_isolated "shared.txt content crossed or missing"
fi
for u in a b; do
  W="$WA"; [ "$u" = b ] && W="$WB"
  S="$(hgit -C "$W" status --porcelain=v1 --untracked-files=all)"
  [ "$S" = " M shared.txt" ] && record PASS "t4${u}_worktree_status" "only ' M shared.txt'" \
    || record FAIL "t4${u}_worktree_status" "unexpected: $(printf '%s' "$S" | tr '\n' ';')"
done
A0="$(cat "$PRIV/t4a.t0")"; A1="$(cat "$PRIV/t4a.t1")"; B0="$(cat "$PRIV/t4b.t0")"; B1="$(cat "$PRIV/t4b.t1")"
MAXS="$A0"; [ "$B0" -gt "$MAXS" ] && MAXS="$B0"
MINE="$A1"; [ "$B1" -lt "$MINE" ] && MINE="$B1"
if [ "$MAXS" -le "$MINE" ]; then record INFO t4_overlap "runs overlapped in time (second granularity)"; else record INFO t4_overlap "runs did NOT overlap; repeat if you need timing evidence"; fi
FP2="$(fingerprint)"
if [ "$FP2" = "$FP0" ]; then record PASS source_unchanged_after_t4 "source fingerprint identical"; else record FAIL source_unchanged_after_t4 "source checkout changed"; fi

# ------------- T5: ADR-0024 mechanics on real Codex output (no Codex involved)
echo "== T5 snapshot commits, merge-tree integration, cleanup"
snap() { # worktree ref
  local t c
  hgit -C "$1" add -A
  t="$(hgit -C "$1" write-tree)"
  c="$(hgit -C "$1" commit-tree "$t" -p "$BASE" -m unit)"
  hgit -C "$SRC" update-ref "$2" "$c"
}
snap "$W1" refs/agentplane/spike/units/one
snap "$WA" refs/agentplane/spike/units/a
snap "$WB" refs/agentplane/spike/units/b
set +e
OUT_CLEAN="$(hgit -C "$SRC" merge-tree --write-tree --name-only --no-messages --merge-base="$BASE" refs/agentplane/spike/units/one refs/agentplane/spike/units/a)"; RC_CLEAN=$?
OUT_CONF="$(hgit -C "$SRC" merge-tree --write-tree --name-only --no-messages --merge-base="$BASE" refs/agentplane/spike/units/a refs/agentplane/spike/units/b)"; RC_CONF=$?
set -e
[ "$RC_CLEAN" = 0 ] && record PASS t5_merge_disjoint "disjoint units merge cleanly" || record FAIL t5_merge_disjoint "exit $RC_CLEAN"
if [ "$RC_CONF" = 1 ] && printf '%s\n' "$OUT_CONF" | grep -qx 'shared.txt'; then
  record PASS t5_merge_conflict "same-file units conflict and name shared.txt"
else
  record FAIL t5_merge_conflict "exit $RC_CONF"
fi
for n in one a b; do hgit -C "$SRC" worktree remove --force "$WT/agentplane-spike-$n"; done
LISTED="$(hgit -C "$SRC" worktree list --porcelain | grep -c '^worktree ')"
REFS="$(hgit -C "$SRC" for-each-ref refs/agentplane | wc -l | tr -d ' ')"
if [ "$LISTED" = 1 ] && [ "$REFS" = 3 ] && [ -z "$(ls -A "$WT")" ]; then
  record PASS t5_cleanup "worktrees removed; 3 unit refs kept"
else
  record FAIL t5_cleanup "worktrees=$LISTED refs=$REFS leftover=$(ls "$WT" | tr '\n' ' ')"
fi
FP3="$(fingerprint)"
if [ "$FP3" = "$FP0" ] && [ "$(hgit -C "$SRC" rev-parse HEAD)" = "$BASE" ]; then
  record PASS source_unchanged_at_end "HEAD and working tree identical to baseline"
else
  record FAIL source_unchanged_at_end "source changed"
fi

echo
echo "results: $RESULTS"
echo "raw Codex output (local only, do not commit or paste): $PRIV"
if [ "$FAILS" -eq 0 ]; then echo "SPIKE VERDICT: PASS (see INFO rows for the decision table)"; else echo "SPIKE VERDICT: FAIL ($FAILS failing checks)"; fi
[ "$FAILS" -eq 0 ]
