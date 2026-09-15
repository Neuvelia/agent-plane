.PHONY: fmt test vet check build clean

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
