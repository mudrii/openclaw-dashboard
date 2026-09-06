.PHONY: build build-debug test frontend-test container-test lint vet clean all staticcheck cover check fmt govulncheck

BINARY := openclaw-dashboard
CONTAINER_ENGINE ?= docker
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")

# Pinned govulncheck version — must match .github/workflows/tests.yml so local
# and CI scans agree.
GOVULNCHECK_VERSION := v1.3.0
# Staticcheck 2026.2.1 supports Go 1.27 export data; CI uses this target too.
STATICCHECK_VERSION := v0.8.1

# Release builds disable CGO for static-link parity with Docker, GoReleaser,
# and Nix. The race-test target overrides this because Linux race builds need
# cgo even though the shipped binary does not.
export CGO_ENABLED := 0

all: lint test build

# Production build — strips DWARF symbols (-s -w) for minimal artefact size.
# Matches the binary shipped by Dockerfile + .goreleaser.yml + flake.nix.
build:
	go build -trimpath -ldflags="-s -w -X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)" -o $(BINARY) ./cmd/openclaw-dashboard

# Debug build — keeps DWARF so stack traces in panics are usable. Use locally
# when investigating crashes; do not ship a debug binary as a release artefact.
build-debug:
	go build -trimpath -ldflags="-X github.com/mudrii/openclaw-dashboard.BuildVersion=$(VERSION)-debug" -o $(BINARY)-debug ./cmd/openclaw-dashboard

test:
	CGO_ENABLED=1 go test -race -count=1 ./...

frontend-test:
	node scripts/frontend-regression.cjs

# Separate from make check: requires an available Docker or Podman engine.
container-test:
	$(CONTAINER_ENGINE) build -t openclaw-dashboard-test .
	$(CONTAINER_ENGINE) run --rm --network none --entrypoint /bin/sh \
		-v "$(CURDIR)/scripts/container-smoke.sh:/tmp/container-smoke.sh:ro" \
		openclaw-dashboard-test /tmp/container-smoke.sh

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

# Run govulncheck via `go run` so first-time contributors don't need a prior
# `go install ...`. The version is pinned to match CI.
govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

clean:
	rm -f $(BINARY) $(BINARY)-debug

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

check: frontend-test vet lint test govulncheck staticcheck build
