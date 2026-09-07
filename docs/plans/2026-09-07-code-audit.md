# Code audit — 2026-09-07

Scope: correctness, quality, test coverage and documentation across the root CLI/facade, all eight internal Go packages, embedded frontend, scripts, packaging and CI/release workflows. Baseline: `4e04a2074d3270211dc5c84b178aa39835f1b7a5`, a clean working tree with `VERSION=v2026.9.6`. The fixes below are uncommitted, unreleased changes on top of that baseline.

This was a risk-based source audit backed by regression tests and local/container execution. It is not exhaustive branch coverage, a proof that every code path is correct, or live OpenClaw release certification.

## Confirmed findings and repairs

| Priority | Finding and consequence | Repair and evidence |
|----------|-------------------------|---------------------|
| P1 | Chat relied on CORS response headers without checking the browser's Origin before work. A foreign site's simple POST could reach the authenticated gateway path. | `appserver` now rejects foreign/opaque origins before reading dashboard context or invoking the gateway. Tests cover foreign hosts, userinfo, paths, queries/fragments, wrong ports, loopback development, origin-less clients and a Host-preserving TLS proxy; a fake transport verifies whether gateway work occurs. |
| P2 | Snapshot and token-cache writers shared predictable `.tmp` filenames. A second writer could retain a handle to the inode already renamed to the published file, then corrupt it. A pre-existing permissive temp file also preserved its old permissions. | Both writers use a unique 0600 temporary inode, sync/close it, then rename it. Tests hold the old temp file open, publish, write corruption through the other handle, and verify the published JSON and permissions survive. A concurrent-reader regression checks 16 simultaneous writers, complete payload visibility, private permissions and cleanup. Rename-failure tests verify the existing destination survives and temporary files are removed. |
| P2 | Modern model-cost chart construction assigned by display name. Two providers using the same model label produced a total of 5 but a breakdown of only 3 in the reproduction. | Shared display-name costs are summed. The regression verifies the model breakdown remains 5, matching the day total. Raw model identities remain available in token rows. |
| P2 | The refresh wrapper manufactured `OPENCLAW_HOME`, rejected missing host state before the binary could select a container/profile, and did not find the extracted release's root binary from `assets/runtime`. | The wrapper delegates state selection/validation to the binary, preserves the environment, and resolves source/release binary locations. Executable shell fixtures cover default/existing native state, explicit home/state, container selection without a host mount, and extracted releases. |
| P2 | The runtime image lacked procps-compatible `ps`, timezone data, Bash and Git. The baseline image failed its process smoke test and failed to preserve a configured IANA timezone when tested independently. | Added the required OS packages. A network-isolated smoke test exercises the shipped non-root image, process tooling, one-shot refresh, Asia/Kuala_Lumpur preservation and 0600 output. `make container-test` runs in both CI and release validation. |
| P2 | Maintained docs still described legacy-only sources as universal, non-null modern costs, a 20-row modern session limit, obsolete model-name precedence, a seven-module frontend, an incorrect linker symbol and frontend edits without a rebuild. | Updated README, architecture, technical, contributor, configuration and infrastructure references. Legacy behavior is identified separately from runtime RPC behavior; new CI checks and operational requirements are documented. |

Implementation/regression entrypoints:

- [Chat origin validation](../../internal/appserver/server_chat_handler.go) and [request tests](../../internal/appserver/server_chat_origin_test.go).
- [Snapshot publication](../../internal/apprefresh/refresh.go), [collector regression](../../internal/apprefresh/refresh_collector_test.go), and [cache regression](../../internal/apprefresh/token_usage_cache_save_test.go), and [concurrent publication tests](../../internal/apprefresh/snapshot_concurrency_test.go).
- [Modern usage/chart projection](../../internal/apprefresh/runtime_usage.go) and [usage tests](../../internal/apprefresh/runtime_usage_test.go).
- [Refresh wrapper](../../assets/runtime/refresh.sh) and [executable fixtures](../../refresh_script_test.go).
- [Runtime image](../../Dockerfile), [container smoke](../../scripts/container-smoke.sh), and [canonical Make targets](../../Makefile).

Additional finding during fix validation: the 30-day cost chart rendered every value label and oversized low-value marker, producing overlaps on mobile. Dense ranges now retain all data points and exact-value tooltips, omit inline value labels, and cap marker radius relative to spacing. The seven-day view retains its value labels. The new embedded-renderer regression failed before this repair and passed afterward.

## Review coverage

| Area | What was assessed |
|------|-------------------|
| Configuration/runtime | Defaults and clamps, target validation, native/container/profile routing, environment inheritance, credential resolution and Homebrew seeding. |
| Runtime adapter | Read/write allowlists, shell-free argv, command deadlines, stdout/stderr bounds, error classification and redaction. |
| Collection/aggregation | RPC pagination and row caps, completeness and nulls, selected-target isolation, stale retention, usage totals/charts, legacy transcript/cache behavior and atomic publication. |
| HTTP/chat/operations | Request validation, CORS/origins, body limits, cache coherence, lifecycle ownership, log sharing, static allowlists, chat capability and operation authorization/revision/audit paths. |
| System/service | Platform-specific process/metrics behavior, version probes, timeouts, lifecycle propagation, service file permissions, quoting and install/uninstall tests. |
| Frontend | Embedded module/dirty-state wiring, missing/stale data treatment, escaping, pagination, runtime panels and operation controls; actual embedded-function and CSS regression harness executed. |
| Packaging/CI/docs | Go/runtime dependency policy, build flags, immutable action references, local/CI/release command alignment, Docker runtime dependencies and maintained documentation contracts. |

The Go runtime remains dependency-free: no `require` block or `go.sum` was introduced. New production logic remains in owning internal packages; the root CLI composition is documented separately from forwarding wrappers. No broad refactor or toolchain bump was needed.

## Validation evidence

Host: macOS arm64, Go 1.27.1. Linux: arm64 under Podman, using the exact Dockerfile Go image digest `sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125`.

| Check | Outcome |
|-------|---------|
| Failing regressions before repairs | Reproduced chart overwrite, chat origin bypass, refresh-wrapper failures, shared-temp corruption/permissions, missing image process tooling and timezone fallback. |
| `make frontend-test` | Passed: 46 embedded JavaScript/CSS regression cases. |
| `make vet`, `make lint` | Passed; golangci-lint reported zero issues. |
| `make test` on macOS | Full race suite passed. Initial parallel coverage/race activity caused two subprocess version-test failures; three isolated reruns and subsequent full race runs passed. Test timeouts were not relaxed. |
| Linux `CGO_ENABLED=1 go test -race -count=1 ./...` | Passed across all ten packages after provisioning build-base, Bash, procps, Git and Node in an ephemeral test container. Linux-specific service and metrics tests executed. |
| Linux frontend harness | Passed all 46 cases. |
| `make build` | Passed with repository release flags and BuildVersion. |
| Pinned govulncheck v1.3.0 | Passed on Linux: “No vulnerabilities found.” Host tool downloads timed out; the scan ran successfully in the network-enabled tool container. |
| Pinned standalone Staticcheck v0.8.1 | Passed on Linux. Host module download timed out; no pin was changed or check weakened. |
| ShellCheck, severity warning | Passed for install, uninstall, refresh and container-smoke scripts using the available ShellCheck image. |
| Runtime image build/smoke | Rebuilt and passed under Podman. Smoke execution has networking disabled and a fixture runtime; it makes no live gateway call. |
| `make cover` | Passed, producing `coverage.out` and `coverage.html`; measured aggregate statement coverage 90.6%. |

Local command logs use `/tmp/openclaw-full-audit-*.log`; generated coverage files are ignored build artifacts. The matrix records component outcomes across host and container environments, not a claim that the host's network-dependent `make check` finished successfully.

## Coverage assessment

The macOS `make cover` profile reports statement coverage, not branch coverage. Its baseline aggregate was 90.3%; the repaired source profile reports 90.6%.

| Package | Statements covered |
|---------|-------------------:|
| Root CLI/facade | 63.7% |
| `cmd/openclaw-dashboard` | 50.0% |
| `appchat` | 94.7% |
| `appconfig` | 86.0% |
| `appopenclaw` | 87.7% |
| `apprefresh` | 92.9% |
| `appruntime` | 77.7% |
| `appserver` | 92.8% |
| `appservice` | 91.6% |
| `appsystem` | 90.4% |

Coverage is strong in the central data and HTTP paths, with meaningful tests for unavailable/partial results, pagination, target isolation, redaction and concurrency. The uncovered `cmd.main` launcher and root composition paths require interpretation: subprocess tests exercise binaries without contributing those child-process statements to the parent profile. Likewise, `HasExactActiveRun` reports zero in its own package profile but is exercised by the server's abort-operation tests. These numbers should not be treated as proof those behaviors are untested.

Remaining test investment should focus on deterministic subprocess timing and live native/Docker/Podman acceptance. The follow-up validation below adds cross-package coverage and fixture-backed browser rendering/interaction. A percentage gate alone would not have found the reproduced chart, origin, publication or image failures.

## Fix validation follow-up

All original six findings above and the additional browser finding have an implemented repair; no identified code or documentation defect is deferred. The follow-up strengthened snapshot regressions and revalidated the complete repaired working tree:

| Check | Final result |
|-------|--------------|
| macOS `make frontend-test vet lint test build` | Passed, including the full race suite. After the browser-discovered frontend-only repair, `make frontend-test build` passed with **47** frontend cases and rebuilt the embedded SPA; final `make vet lint test` also passed across all ten packages. |
| Linux `make check` | Passed as one canonical invocation in an isolated source copy using the Dockerfile builder image and pinned golangci-lint v2.13.2. This includes frontend tests, vet, lint, race tests, govulncheck, Staticcheck and build. This full gate preceded the additional frontend-only repair; its regression and rebuild were checked afterward. No vulnerabilities or lint issues reported. |
| `go test -race -count=5 ./internal/apprefresh -run TestWriteSnapshotAtomic` | Passed all five repetitions, including simultaneous writers/readers and preservation on failed publication. |
| Linux `go test -coverpkg=./... -coverprofile=coverage-integration.out ./...` | Passed; aggregate cross-package statement coverage **91.5%**. This combines instrumentation across package tests and is a different measurement from the earlier macOS per-package 90.6% profile. |
| `BUILDAH_FORMAT=docker make container-test CONTAINER_ENGINE=podman` | Passed again with the final runtime image: process tools, configured timezone, real collector invocation and private snapshot. |

Follow-up logs are `/tmp/openclaw-fixes-{macos-gate,final-host,linux-gate,concurrency,ui-gate,container,browser}.log`. Coverage output is in the isolated ignored `dist/openclaw-fix-validation.*` checkout. These are local execution artifacts, not committed dependencies or hosted CI results.

Browser validation used headless Chrome with the rebuilt Go executable and isolated fake OpenClaw CLI fixtures for both native and container target selection. Both passed at 1440×1000 and 390×844: collected and rendered combined model cost of $5.00, 30-day selection, visible mobile chart, disabled chat controls, and zero JavaScript page errors. Screenshots were inspected; the mobile chart overlap was repaired and retested. Unpopulated fixture inventories remain deliberately unavailable. This does not establish live runtime or cross-browser compatibility.

## Remaining limits and follow-up

- No live native, Docker or Podman gateway acceptance matrix was run for these changes. The Docker daemon was unavailable locally; Podman executed Linux tests and image checks. This is packaging/platform evidence, not live gateway certification.
- The embedded frontend harness uses a lightweight DOM fixture. The additional Chrome checks cover the affected chart and basic controls on desktop/mobile using native/container fixtures; they are not a complete cross-browser or live runtime matrix.
- Service install/start/stop behavior was tested with fixtures, without replacing the user's running services. The audit did not submit real chat inference, enable operations, mutate gateway configuration, or execute a real automation.
- The latest observed successful main-branch Tests run was for `c593d0d66cb7d1bbad38e2a782c1156cd1975305` ([run 34025877750](https://github.com/mudrii/openclaw-dashboard/actions/runs/34025877750)). No remote workflow run was found for baseline `4e04a20`; local uncommitted changes have not been remotely tested or released. The new Container smoke workflow and suggested branch-protection context are only local changes until published; remote branch protection was not changed.
- Read access remains unauthenticated and relies on trusted network exposure. Optional operation reservations retain only the newest 500 audit records; deleting/pruning one removes replay protection for that ID. These are documented product constraints, not security certification.
- Unique temporary files prevent cross-writer corruption but do not order snapshots by collection start time. Separate processes publishing to one dashboard directory follow completion order. File sync plus rename does not guarantee parent-directory durability through power loss.

Audit disposition: the five original code/packaging defect groups, documentation drift, and additional dense-chart rendering defect are repaired. Validation limits remain explicit; no claim of exhaustive correctness or release approval is made.
