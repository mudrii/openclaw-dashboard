# OpenClaw Dashboard — Go Runtime Verification

The dashboard ships as a Go binary only. The old Python comparison material is no
longer part of the active documentation set; use git history if you need the
migration-era benchmark notes.

## Current verification targets

- Single-binary server startup and refresh flow
- `go test` and `go test -race` coverage for the full Go codebase
- Release-style builds for `darwin` and `linux` on `amd64` and `arm64`
- Packaged installs carrying the runtime assets the binary expects:
  `config.json`, `themes.json`, `refresh.sh`, `VERSION`, and example configs

## Reproduce locally

```bash
go test -v -count=1 ./...
go test -race -v -count=1 ./...

GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.buildVersion=$(tr -d '\n' < VERSION | sed 's/^v//')" -o dist/openclaw-dashboard-darwin-amd64 .
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.buildVersion=$(tr -d '\n' < VERSION | sed 's/^v//')" -o dist/openclaw-dashboard-darwin-arm64 .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.buildVersion=$(tr -d '\n' < VERSION | sed 's/^v//')" -o dist/openclaw-dashboard-linux-amd64 .
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w -X main.buildVersion=$(tr -d '\n' < VERSION | sed 's/^v//')" -o dist/openclaw-dashboard-linux-arm64 .
```

## Homebrew packaging

Homebrew installs should be validated against the generated formula and release
artifacts:

- `brew install mudrii/tap/openclaw-dashboard`
- first run seeds `~/.openclaw/dashboard`
- `openclaw-dashboard --refresh`
- `openclaw-dashboard --version`

The release workflow and GoReleaser configuration in this repository are the
authoritative packaging definitions for those builds.
