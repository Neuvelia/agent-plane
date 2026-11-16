.PHONY: fmt test vet check verify-changes build clean desktop-check desktop-e2e desktop-stories

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

build:
	mkdir -p bin
	go build -o bin/agentplane ./cmd/agentplane

clean:
	rm -rf bin tmp

# The desktop shell is optional and needs Node.js; `make check` stays Go-only.
desktop-check: build
	cd desktop && npm ci && npm run check

# A virtual display is needed (and available) only on Linux; macOS and Windows
# open a real window. Override with `make desktop-e2e XVFB=` to force none.
XVFB ?= $(if $(shell command -v xvfb-run 2>/dev/null),xvfb-run -a,)

# Drives the real Electron app against bin/agentplane under a virtual display.
desktop-e2e: build
	cd desktop && npm ci && npm run build && $(XVFB) node e2e/demo.cjs

# Opens every Storybook story in a real browser (EN and PL): a11y, no network.
desktop-stories:
	cd desktop && npm ci && (npx playwright install chromium || echo 'browser download failed; set AP_CHROMIUM=/path/to/chrome to use an installed one') && npm run build-storybook && npm run stories:test
