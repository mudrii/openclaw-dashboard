# Homebrew Tap

`openclaw-dashboard` is packaged for Homebrew through a separate tap repository:

- Tap repo: `mudrii/homebrew-tap`
- Install command: `brew install mudrii/tap/openclaw-dashboard`

## How the tap is updated

This repository owns the release automation:

- [`release.yml`](../.github/workflows/release.yml)
- [`.goreleaser.yml`](../.goreleaser.yml)

Tagging a release runs GoReleaser, publishes the release artifacts, and updates
the Homebrew formula in the tap repository. Release tags must point to commits
reachable from `main`, and the tag name must exactly match this repository's
`VERSION` file. Before GoReleaser pushes the tap update, the workflow runs
`make check` plus shellcheck for `assets/runtime/refresh.sh`, `install.sh`, and
`uninstall.sh`.

## Required secrets

The GitHub Actions release job expects:

- repository variable `HOMEBREW_TAP_APP_CLIENT_ID`
- repository secret `HOMEBREW_TAP_APP_PRIVATE_KEY`

The workflow mints a short-lived `HOMEBREW_TAP_TOKEN` from those GitHub App
credentials and uses it to push to `mudrii/homebrew-tap`. The GitHub App
installation must have access to that tap repository with repository contents
write permission; the workflow requests only that permission for the minted
installation token.

`actions/create-github-app-token@v3` uses the GitHub App **Client ID**, not the
numeric App ID. Store it as a repository variable because it is an identifier,
not a secret. Keep the private key in Actions secrets.

Keep the tag and `VERSION` aligned so the binary ldflags, bundled `VERSION`,
release artifacts, and Homebrew formula all describe the same version.

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
