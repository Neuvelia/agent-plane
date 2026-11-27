#!/usr/bin/env sh
set -eu

unformatted=$(gofmt -l ./cmd ./internal ./scripts/verifychanges ./scripts/reviewwarrior)
if [ -n "$unformatted" ]; then
  echo "gofmt required for:"
  echo "$unformatted"
  exit 1
fi

go vet ./...
go test ./...

echo "check: OK"
