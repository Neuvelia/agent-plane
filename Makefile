.PHONY: fmt test vet check verify-changes lint metrics build clean desktop-check desktop-e2e desktop-stories

fmt:
	gofmt -w ./cmd ./internal ./scripts/verifychanges

test:
	go test ./...

vet:
	go vet ./...

check:
	./scripts/check.sh

# Inventories staged, unstaged and untracked paths; does not run checks.
verify-changes:
	go run ./scripts/verifychanges

# Needs golangci-lint v2 (development-only; not in go.mod). `make check` does not
# require it. Findings on code that a change touches fail; legacy findings are
# tolerated until stage 6 of the clean-architecture refactor plan.
lint:
	golangci-lint run ./cmd/... ./internal/...

metrics:
	./scripts/metrics.sh

build:
	mkdir -p bin
	go build -o bin/agentplane$(shell go env GOEXE) ./cmd/agentplane

clean:
	rm -rf bin tmp

# The desktop shell is optional and needs Node.js; `make check` stays Go-only.
desktop-check: build
	cd desktop && npm ci && npm run check

# A virtual display is needed (and available) only on Linux; macOS and Windows
# open a real window. Override with `make desktop-e2e XVFB=` to force none.
XVFB ?= $(if $(shell command -v xvfb-run 2>/dev/null),xvfb-run -a,)
# Directory (relative to desktop/) for e2e screenshots; empty saves none.
E2E_SHOTS ?=

# Drives the real Electron app against bin/agentplane under a virtual display.
desktop-e2e: build
	cd desktop && npm ci && npm run build && $(XVFB) node e2e/demo.cjs $(E2E_SHOTS)

# Opens every Storybook story in a real browser (EN and PL): a11y, no network.
desktop-stories:
	cd desktop && npm ci && (npx playwright install chromium || echo 'browser download failed; set AP_CHROMIUM=/path/to/chrome to use an installed one') && npm run build-storybook && npm run stories:test
