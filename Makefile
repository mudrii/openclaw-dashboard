.PHONY: build test lint vet clean all staticcheck cover check fmt govulncheck

BINARY := openclaw-dashboard
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")

# CGO is disabled so the binary is statically linked and matches the artefacts
# produced by Dockerfile, .goreleaser.yml, and flake.nix. Re-enable CGO only if
# a future feature requires it; ensure all four build paths flip together.
export CGO_ENABLED := 0

all: lint test build

build:
	go build -ldflags="-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)" -o $(BINARY) ./cmd/openclaw-dashboard

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

staticcheck:
	staticcheck ./...

govulncheck:
	govulncheck ./...

clean:
	rm -f $(BINARY)

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

check: vet lint test govulncheck
