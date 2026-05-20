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
      {"context": "PR Validation / Check PR template is filled out"},
      {"context": "Tests / GolangCI-Lint"},
      {"context": "Tests / Go test suite (ubuntu-latest)"},
      {"context": "Tests / Go test suite (macos-latest)"},
      {"context": "Tests / govulncheck"},
      {"context": "Tests / Lint shell scripts"}
    ]
  },
  "enforce_admins": false,
  "required_pull_request_reviews": null,
  "restrictions": null,
  "allow_force_pushes": false,
  "allow_deletions": false
}
EOF
```

Context names follow GitHub's `{workflow name:} / {job name:}` convention, with
matrix values appended in parentheses. If you rename any workflow's top-level
`name:` field or any job's `name:`, mirror the change here and re-run the
command — otherwise the protection rule will silently reference a check that
no longer exists, and PRs will start merging without validation.

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
