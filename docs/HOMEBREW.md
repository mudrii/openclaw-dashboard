# Homebrew Tap

`openclaw-dashboard` is packaged for Homebrew through a separate tap repository:

- Tap repo: `mudrii/homebrew-tap`
- Install command: `brew install mudrii/tap/openclaw-dashboard`

## How the tap is updated

This repository owns the release automation:

- [`release.yml`](../.github/workflows/release.yml)
- [`.goreleaser.yml`](../.goreleaser.yml)

Tagging a release runs GoReleaser, publishes the release artifacts, and updates
the Homebrew formula in the tap repository.

## Required secret

The GitHub Actions release job expects:

- `HOMEBREW_TAP_TOKEN`

That token must be able to push to `mudrii/homebrew-tap`.

## Runtime layout

The formula installs immutable assets into Homebrew `pkgshare`. On first run the
binary seeds a writable runtime directory at:

- `~/.openclaw/dashboard`

That runtime directory is where users should edit:

- `config.json`
- `themes.json`
- `data.json`

The packaged defaults come from this repo's `assets/runtime/` directory and are
installed into `pkgshare` during the release build. Example configs are shipped
under the formula's `examples` directory in `pkgshare`.
