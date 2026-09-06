# Infrastructure Checklist

Post-audit user-side actions. Each command should be run once by a maintainer
on a host that has the relevant tool installed. After completing each item,
tick the box and commit the resulting file change (where applicable).

## 1. Maintain the locked Nix build

`flake.lock` pins nixpkgs and flake-utils. The flake overrides `buildGoModule`
with Go 1.27 explicitly, provides Bash/Git and Linux process tooling, and wraps
the executable with IANA timezone data. `nix flake check --no-update-lock-file`
builds the package and verifies its installed runtime: version, asset seeding,
refresh, timezone and private snapshot permissions. CI runs it on Linux/macOS.

Update inputs deliberately and validate before committing:

```sh
nix flake update
nix flake check --no-update-lock-file --print-build-logs
```

Dependabot does not update flake inputs; review these regularly for fixes.

## 2. Keep Docker base image digests current

`Dockerfile` pins the base images by digest while keeping the human-readable
tags (`golang:1.27-alpine` and `alpine:3.23`) for Dependabot matching. When
refreshing the pins manually, resolve the current digests:

```sh
docker pull golang:1.27-alpine
docker inspect --format='{{index .RepoDigests 0}}' golang:1.27-alpine
# → golang@sha256:<DIGEST_A>

docker pull alpine:3.23
docker inspect --format='{{index .RepoDigests 0}}' alpine:3.23
# → alpine@sha256:<DIGEST_B>
```

Then edit `Dockerfile` if the digests changed:

```dockerfile
FROM golang:1.27-alpine@sha256:<DIGEST_A> AS builder
...
FROM alpine:3.23@sha256:<DIGEST_B>
```

Dependabot's `docker` ecosystem watcher opens PRs when newer digests are
published, so this should normally stay current automatically.

## 3. Enable or refresh branch protection on `main`

CI is hardened but PRs can still be merged without the required checks
passing until branch protection is configured. Run this with the `gh` CLI after
enabling protection for the first time, and re-run it whenever workflow job
names are added, renamed, or removed:

```sh
gh api -X PUT repos/mudrii/openclaw-dashboard/branches/main/protection \
  --input - <<'EOF'
{
  "required_status_checks": {
    "strict": true,
    "checks": [
      {"context": "Check PR template is filled out"},
      {"context": "GolangCI-Lint"},
      {"context": "Go test suite (ubuntu-latest)"},
      {"context": "Go test suite (macos-latest)"},
      {"context": "govulncheck"},
      {"context": "Staticcheck"},
      {"context": "Lint shell scripts"},
      {"context": "Container smoke"},
      {"context": "Release rehearsal (ubuntu-latest)"},
      {"context": "Release rehearsal (macos-latest)"},
      {"context": "Nix flake (ubuntu-latest)"},
      {"context": "Nix flake (macos-latest)"}
    ]
  },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
EOF

# The protection PUT endpoint can leave a default review rule behind. This repo's
# policy is strict required checks without mandatory review approval.
gh api -X DELETE \
  repos/mudrii/openclaw-dashboard/branches/main/protection/required_pull_request_reviews
```

Context names are the check-run `name` values reported by GitHub Actions. For
these workflows that means job names, with matrix values appended in
parentheses. If you rename any job's `name:`, mirror the change here and
re-run the command — otherwise the protection rule can reference a check that
never reports, leaving PRs blocked.

Audit the live required checks before and after changes:

```sh
gh api repos/mudrii/openclaw-dashboard/branches/main/protection/required_status_checks \
  --jq '.checks[].context'
```

## 4. Rehearse before publishing

Run `make check`, `make container-test`, and `make release-check` on the candidate.
`make release-check` requires GoReleaser **v2.4.5**, Node and Go. It validates
configuration, runs the configured frontend/race hooks, builds all four archives
under `dist/release`, verifies their checksums and required assets, then exercises
an extracted native archive. It uses snapshot mode and skips signing/SBOM; it
never publishes a release, changes a tag, or updates the Homebrew tap. CI runs
this rehearsal on PRs and pushes to main, release-readiness and codex branches.
On macOS, `make brew-test` installs the generated formula from local archives in a
uniquely named keg-only fixture, runs its formula test, checks runtime seeding and
refresh, then removes only the fixture. It preserves the existing dashboard
installation and disables Homebrew auto-update, cleanup and autoremove. CI runs
this after the macOS archive rehearsal.

Use `make workflow-test workflow-lint` for the executable publication-gate tests
and pinned actionlint validation.

Snapshot metadata follows GoReleaser's latest reachable tag, so it may differ
from the next release's `VERSION`. A real release requires the exact tag to match
`VERSION`, ancestry from main, and a successful **main push Tests run for that
exact commit**, covering Linux and macOS. Wait for all required checks after
merging; then a maintainer can create the intended release tag. Tagging publishes
artifacts and updates the Homebrew tap. Do not create/delete production tags as
a smoke test.

The rehearsal does not validate GitHub OIDC signing or exercise the tap app's
private key. The real release job installs Syft and Cosign and retains both
signing and SBOM generation. Confirm the repository variable
`HOMEBREW_TAP_APP_CLIENT_ID` and secret `HOMEBREW_TAP_APP_PRIVATE_KEY` are configured
before authorizing publication. See [Homebrew credentials](HOMEBREW.md).

## 5. Sigstore outage runbook

The release pipeline produces a keyless cosign signature on the
`checksums-sha256.txt` artifact (`.goreleaser.yml` `signs:` block). The
signing chain depends on three Sigstore services:

- **Fulcio** — issues short-lived certs from the GitHub OIDC token
- **Rekor** — public transparency log entry for the signature
- **TUF** — root metadata for trust verification

If any of the three is unavailable, the `Run GoReleaser` step in
`release.yml` will fail at the sign stage, blocking the release.

**Recovery options (in order of preference):**

1. **Wait + re-run.** Sigstore outages are usually short (< 1 hour). Check
   <https://status.sigstore.dev>. Re-run the failed workflow run from the
   GitHub Actions UI once status is green.

2. **Keep publication blocked if signing cannot recover.** Investigate the
   failed signing step and retry the same release workflow after service recovery.
   Do not bypass signing with an undocumented environment variable or retag a
   published version. If publication partially succeeded, inspect existing assets
   and the tap commit before choosing a recovery action.

**Verifying a signed release as a user:**

```sh
# Download the archive + checksums + bundle
RELEASE=v2026.5.20
curl -fsSL -o checksums-sha256.txt \
  https://github.com/mudrii/openclaw-dashboard/releases/download/$RELEASE/checksums-sha256.txt
curl -fsSL -o checksums-sha256.txt.bundle \
  https://github.com/mudrii/openclaw-dashboard/releases/download/$RELEASE/checksums-sha256.txt.bundle

# Verify the bundle came from this repo's release workflow
cosign verify-blob \
  --bundle checksums-sha256.txt.bundle \
  --certificate-identity-regexp '^https://github.com/mudrii/openclaw-dashboard/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums-sha256.txt

# Then verify each archive against the (now-trusted) checksums file
sha256sum -c checksums-sha256.txt
```
