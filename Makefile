.PHONY: build build-debug test lint vet clean all staticcheck cover check fmt govulncheck

BINARY := openclaw-dashboard
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")

# Pinned govulncheck version — must match .github/workflows/tests.yml so local
# and CI scans agree.
GOVULNCHECK_VERSION := v1.3.0

# CGO is disabled so the binary is statically linked and matches the artefacts
# produced by Dockerfile, .goreleaser.yml, and flake.nix. Re-enable CGO only if
# a future feature requires it; ensure all four build paths flip together.
export CGO_ENABLED := 0

all: lint test build

# Production build — strips DWARF symbols (-s -w) for minimal artefact size.
# Matches the binary shipped by Dockerfile + .goreleaser.yml + flake.nix.
build:
	go build -ldflags="-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)" -o $(BINARY) ./cmd/openclaw-dashboard

# Debug build — keeps DWARF so stack traces in panics are usable. Use locally
# when investigating crashes; do not ship a debug binary as a release artefact.
build-debug:
	go build -ldflags="-X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)-debug" -o $(BINARY)-debug ./cmd/openclaw-dashboard

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

# Run govulncheck via `go run` so first-time contributors don't need a prior
# `go install ...`. The version is pinned to match CI.
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

clean:
	rm -f $(BINARY) $(BINARY)-debug

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

check: vet lint test govulncheck
