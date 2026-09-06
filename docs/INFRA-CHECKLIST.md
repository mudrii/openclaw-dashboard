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
      {"context": "Container smoke"}
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

## 4. (Optional) Verify release pipeline end-to-end

The `release.yml` workflow installs `syft` (SBOM), `cosign` (keyless signing),
and `shellcheck`, then rejects release tags whose commit is not reachable from
`origin/main` or whose tag does not exactly match `VERSION`. Use a real
release-candidate commit on `main` for end-to-end verification:

```sh
version="$(tr -d '[:space:]' < VERSION)"
git fetch origin main
git merge-base --is-ancestor HEAD origin/main
git tag "$version"
git push origin "$version"

# Watch the run. If this was only a gate rehearsal and no release should remain:
gh release delete "$version" --yes --cleanup-tag
git push origin ":$version"
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
