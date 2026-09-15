#!/usr/bin/env sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 slug"
  echo "example: $0 event-store-choice"
  exit 2
fi

slug=$1
last=$(find docs/adr -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9]-*.md' ! -name '0000-template.md' | sort | tail -n 1 || true)

if [ -z "$last" ]; then
  next=1
else
  base=$(basename "$last")
  number=${base%%-*}
  number=$(printf '%s' "$number" | sed 's/^0*//')
  if [ -z "$number" ]; then
    number=0
  fi
  next=$((number + 1))
fi

num=$(printf '%04d' "$next")
path="docs/adr/${num}-${slug}.md"

sed \
  -e "s/{{ADR_NUMBER}}/$num/g" \
  -e "s/{{ADR_TITLE}}/$slug/g" \
  docs/adr/0000-template.md > "$path"

echo "$path"
