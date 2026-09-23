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
`VERSION` file. Before GoReleaser pushes the tap update, the release workflow
requires, in order:

- a completed, successful `Tests` workflow run (`tests.yml`) on `main` for the
  exact tagged commit;
- `make check`;
- `shellcheck --severity=warning` for `assets/runtime/refresh.sh`,
  `install.sh`, `uninstall.sh`, and `scripts/container-smoke.sh`;
- `make container-test` (builds the runtime image and runs its smoke test).

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

`config.json`, `themes.json`, `refresh.sh`, and `examples/config.minimal.json`
are copied only when missing, so user edits survive upgrades. `VERSION` is
overwritten on every start so the runtime directory tracks the installed
release. Users edit `config.json` and `themes.json` there; the collector writes
`data.json` there.

Service definitions created by `openclaw-dashboard install` record the stable
`$(brew --prefix)/opt/openclaw-dashboard/bin/openclaw-dashboard` path rather
than the versioned `Cellar` path, so they survive `brew upgrade`; run
`openclaw-dashboard restart` after an upgrade to load the new binary.

The packaged defaults come from this repo's `assets/runtime/` directory and are
installed into `pkgshare` during the release build. Example configs are shipped
under the formula's `examples` directory in `pkgshare`.
