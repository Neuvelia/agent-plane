#!/usr/bin/env sh
set -eu

if [ "$#" -ne 2 ]; then
  echo "usage: $0 FEATURE_ID slug"
  echo "example: $0 F0.3 data-bindings"
  exit 2
fi

feature_id=$1
slug=$2
path="docs/features/${feature_id}-${slug}.md"

if [ -e "$path" ]; then
  echo "file already exists: $path"
  exit 1
fi

sed \
  -e "s/{{FEATURE_ID}}/$feature_id/g" \
  -e "s/{{FEATURE_NAME}}/$slug/g" \
  docs/features/_template.md > "$path"

echo "$path"
