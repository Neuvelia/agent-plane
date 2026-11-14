.PHONY: fmt test vet check build clean desktop-check desktop-e2e

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

vet:
	go vet ./...

check:
	./scripts/check.sh

build:
	mkdir -p bin
	go build -o bin/agentplane ./cmd/agentplane

clean:
	rm -rf bin tmp

# The desktop shell is optional and needs Node.js; `make check` stays Go-only.
desktop-check: build
	cd desktop && npm ci && npm run check

# Drives the real Electron app against bin/agentplane under a virtual display.
desktop-e2e: build
	cd desktop && npm ci && npm run build && xvfb-run -a node e2e/demo.cjs
