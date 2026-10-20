#!/usr/bin/env sh
set -eu

unformatted=$(gofmt -l ./cmd ./internal)
if [ -n "$unformatted" ]; then
  echo "gofmt required for:"
  echo "$unformatted"
  exit 1
fi

go vet ./...
go test ./...

if [ -f desktop/package.json ]; then
  if [ ! -d desktop/node_modules ]; then
    echo "desktop dependencies missing: run npm ci in desktop/"
    exit 1
  fi
  (cd desktop && npm run typecheck && npm test && npm run build)
fi

echo "check: OK"
