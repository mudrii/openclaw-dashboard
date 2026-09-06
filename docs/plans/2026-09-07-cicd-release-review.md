# CI/CD and installation release review — 2026-09-07

Baseline: `b26c90b` on `release-readiness`. This review extends the [code audit](2026-09-07-code-audit.md) to hosted CI, publication gates, archives, Homebrew, containers and Nix. It prepares the candidate for release; it does not authorize merging, tagging, publishing, or updating the production Homebrew tap.

## Findings and repairs

1. **Maintenance updates failed unrelated tests.** Exact action SHAs and Docker digests were duplicated in assertions. The live Dependabot Tests run 34025970324 failed on the Syft SHA assertion. Tests now require the intended action/tool contract and immutable SHA/digest format without freezing one valid revision forever.
2. **Release branches had no push CI and archives were first exercised during publication.** Tests now support release-readiness/codex pushes and manual dispatch. Linux/macOS release rehearsals validate GoReleaser config, run frontend/race hooks, build four target archives, verify checksums and assets, and exercise the extracted native archive without publishing. Output is confined to `dist/release` so cleanup preserves other local build artifacts.
3. **Tag publication did not require the full platform CI result.** Release now requires the latest successful main-push Tests run for the exact tagged commit, with explicit read permission. Seven executable gate scenarios cover success and rejection states. Publication/tap updates are serialized across tags.
4. **Installed Homebrew behavior was not tested.** macOS CI installs the generated formula from local archives using a unique keg-only identity, runs its formula test, and verifies runtime seeding, refresh, timezone and private snapshots. It retains the generated install logic and preserves any existing dashboard installation. Auto-update, cleanup and autoremove are disabled.
5. **Nix selected the wrong Go compiler and had no lock file.** A real Linux build reproduced Go 1.26.7 rejecting the Go 1.27 module. The flake now overrides the builder with `go_1_27`, locks inputs, packages Linux process tooling and timezone data, and includes an installed-runtime check. Linux/macOS CI runs the locked flake checks.
6. **Container acceptance stopped at one-shot collection.** The non-root, network-isolated smoke now also starts the HTTP server and checks the embedded UI and refresh API. Generated `dist/` output is excluded from Docker build context.
7. **Release documentation and required-check guidance lagged behavior.** Updated contributor/install instructions, the PR template, changelog and infrastructure checklist. Removed publish/delete/retag rehearsal instructions and the undocumented signing-bypass recommendation. Publication keeps signing and SBOM generation enabled.

## Local validation

- macOS arm64: frontend (47 cases), workflow gate (7 cases), vet, lint (zero issues), complete ten-package race suite and build passed.
- GoReleaser v2.4.5: config check and complete four-archive snapshot rehearsal passed; checksums/assets and extracted macOS arm64 refresh passed.
- Generated Homebrew formula: install, formula test, seeded runtime assets and refresh passed. The existing `openclaw-dashboard` 2026.7.12 installation remains present. An initial fixture-cleanup run triggered Homebrew autoremove of `unbound`; it was restored to 1.26.0, and the fixture now disables autoremove.
- Linux arm64 under Podman: pinned Nix flake build and installed-runtime check passed. Hosted CI supplies additional Linux amd64/macOS coverage.
- Final container build and smoke passed, including HTTP UI/API, process tools, timezone, refresh and private snapshot.
- Actionlint v1.7.12 and ShellCheck passed. No Go module runtime dependencies were added.

Local evidence: `/tmp/openclaw-cicd-final-host.log`, `/tmp/openclaw-release-rehearsal.log`, `/tmp/openclaw-brew-smoke.log`, `/tmp/openclaw-nix-check.log`, `/tmp/openclaw-cicd-container.log`. These are local artifacts, not hosted CI results.

## Publication boundary

The candidate must pass the expanded hosted Tests workflow before it is considered ready to merge. The main branch must then pass that workflow for the merged commit before release tagging. Signing/OIDC and authenticated tap publication are exercised only by an authorized real release; the snapshot deliberately skips signing and SBOM and never writes to the tap. Repository credential names were checked for presence, not private-key validity. Live OpenClaw inference and gateway mutations are outside these packaging fixtures.
