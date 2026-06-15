# Infrastructure Checklist

Post-audit user-side actions. Each command should be run once by a maintainer
on a host that has the relevant tool installed. After completing each item,
tick the box and commit the resulting file change (where applicable).

## 1. Generate `flake.lock` for reproducible Nix builds

`flake.lock` is not committed. Without it the flake floats whatever nixpkgs
revision the user's local CLI happens to cache, breaking reproducibility.

```sh
nix flake update
git add flake.lock
git commit -m "build(nix): commit flake.lock for reproducible builds"
```

Re-run `nix flake update` periodically (monthly is a reasonable cadence) to
pick up nixpkgs security updates. The dependabot config in
`.github/dependabot.yml` does not cover flake inputs today.

## 2. Pin Docker base image digests

`Dockerfile` uses floating tags `golang:1.26-alpine` and `alpine:3.21`. A
hostile registry compromise (or accidental tag move) could swap the image
under us. Resolve the current digests and pin them:

```sh
docker pull golang:1.26-alpine
docker inspect --format='{{index .RepoDigests 0}}' golang:1.26-alpine
# → golang@sha256:<DIGEST_A>

docker pull alpine:3.21
docker inspect --format='{{index .RepoDigests 0}}' alpine:3.21
# → alpine@sha256:<DIGEST_B>
```

Then edit `Dockerfile`:

```dockerfile
FROM golang:1.26-alpine@sha256:<DIGEST_A> AS builder
...
FROM alpine:3.21@sha256:<DIGEST_B>
```

Dependabot's `docker` ecosystem watcher will open PRs when newer digests are
published, so this stays current automatically.

## 3. Enable branch protection on `main`

CI is hardened but PRs can still be merged without the required checks
passing. Run once with the `gh` CLI:

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
      {"context": "Lint shell scripts"}
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

## 4. (Optional) Verify release pipeline end-to-end

The `release.yml` now installs `syft` (SBOM) and `cosign` (keyless signing).
Trigger a dry-run tag on a throwaway branch to confirm the pipeline works
before the next real release:

```sh
git checkout -b release-dryrun
git tag v0.0.0-dryrun
git push origin v0.0.0-dryrun

# Watch the run; if it succeeds, delete the tag:
gh release delete v0.0.0-dryrun --yes --cleanup-tag
git push origin :v0.0.0-dryrun
```

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

2. **Skip signing for this release.** Add a job-level env var to bypass
   the signs block:

   ```yaml
   # release.yml — temporary while Sigstore is down
   env:
     GORELEASER_SKIP: sign
   ```

   Push a no-op commit, retag, and re-release. **Remove the env var the
   moment Sigstore is restored** — unsigned releases are not a steady state.

3. **Manual SHA-256 verification only.** Document in the GitHub Release
   body that the cosign bundle is missing for this version and that users
   should verify via `sha256sum -c checksums-sha256.txt` against the
   archives. Acceptable as a one-off; do not normalize.

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
