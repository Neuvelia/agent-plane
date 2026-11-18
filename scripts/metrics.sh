#!/usr/bin/env sh
# Reports size metrics from AGENTS.md "Clean code rules" for production Go code:
# files over the 300-line target / 500-line hard limit and functions over the
# 30-line target / 50-line hard limit. Report only; golangci-lint (make lint)
# and internal/archtest enforce. Usage: scripts/metrics.sh [--strict]
# --strict exits 1 when any hard limit is exceeded.
set -eu

files=$(find cmd internal -name '*.go' ! -name '*_test.go' | sort)

echo "== files over 300 lines (hard limit 500) =="
for f in $files; do
  n=$(wc -l < "$f")
  [ "$n" -gt 300 ] && printf '%5d %s%s\n' "$n" "$f" "$([ "$n" -gt 500 ] && echo '  HARD')"
done | sort -rn

echo "== functions over 30 lines (hard limit 50) =="
# gofmt guarantees a top-level function ends at a line that is exactly "}".
for f in $files; do
  awk -v file="$f" '
    /^func / { name = $0; start = NR }
    /^}$/ && start { n = NR - start + 1; if (n > 30) printf "%5d %s:%d %s%s\n", n, file, start, substr(name, 1, 70), (n > 50 ? "  HARD" : ""); start = 0 }
  ' "$f"
done | sort -rn

if [ "${1:-}" = "--strict" ]; then
  hard=$(for f in $files; do
    [ "$(wc -l < "$f")" -gt 500 ] && echo "$f"
    awk '/^func /{s=NR} /^}$/&&s{if(NR-s+1>50)print FILENAME; s=0}' "$f"
  done | wc -l)
  [ "$hard" -eq 0 ] || { echo "metrics: $hard hard-limit breaches"; exit 1; }
fi
