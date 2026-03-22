.PHONY: build test lint vet clean all staticcheck cover check

BINARY := openclaw-dashboard
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")

all: lint test build

build:
	go build -o $(BINARY) ./cmd/openclaw-dashboard

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

staticcheck:
	staticcheck ./...

clean:
	rm -f $(BINARY)

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

check: vet lint test
