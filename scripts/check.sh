#!/usr/bin/env sh
set -eu

unformatted=$(gofmt -l ./cmd ./internal ./scripts/verifychanges ./scripts/roadmapcheck ./scripts/reviewwarrior)
if [ -n "$unformatted" ]; then
  echo "gofmt required for:"
  echo "$unformatted"
  exit 1
fi

go vet ./...
go test ./...
# Roadmap registry, evidence and STATUS.md (ADR-0038); warnings do not fail.
go run ./scripts/roadmapcheck

echo "check: OK"
