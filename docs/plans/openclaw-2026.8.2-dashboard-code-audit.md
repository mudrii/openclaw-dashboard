# OpenClaw 2026.8.2 dashboard compatibility: source audit

Date: 2026-09-05. Scope: read-only analysis of the dashboard source and selected non-secret configuration fields in `$HOME/.openclaw/openclaw.json`. This document is the code audit supporting the wider release/runtime investigation. No implementation or configuration was changed and no tests or OpenClaw probes were run for this sub-audit.

Version boundary: the coordinating runtime investigation identifies a running OpenClaw 2026.8.2 container and a newer host CLI. Do not attribute host-only features to 2026.8.2. The findings below establish dashboard behavior from its actual source; the wider report must confirm the release-specific commands and payloads against the running version before implementation.

## Highest-priority findings

### 1. Sessions are still enumerated from the legacy JSON store

`loadSessionStores` discovers only `agents/*/sessions/sessions.json`. `collectSessions` iterates those file entries, rejects entries without a session ID, and hides entries older than 24 hours. It calls the CLI only for a map of session key to model. Consequently, a successful modern `sessions --all-agents --limit all --json` response cannot create a missing session row. A migrated SQLite store produces missing sessions even when the CLI sees them. This also affects active-session counts, context alerts, group-name enrichment, and parent/child visualization.

Evidence: [refresh_sessions.go:21](../../internal/apprefresh/refresh_sessions.go#L21), [refresh_sessions.go:231](../../internal/apprefresh/refresh_sessions.go#L231), [session_model_cache.go:143](../../internal/apprefresh/session_model_cache.go#L143), [refresh.go:338](../../internal/apprefresh/refresh.go#L338), [web/index.html:1766](../../web/index.html#L1766).

Implementation seam: replace the model-only CLI projection with a bounded session snapshot collector under `internal/apprefresh`; normalize its records before existing presentation and aggregation. Keep the legacy reader as a clearly identified compatibility source. Select one authoritative source per scope; do not merge old files with current snapshots indiscriminately. Carry source, collection time, errors, and completeness independently of the session count.

Acceptance coverage: SQLite-era CLI fixture with no JSON files still yields session rows; multiple agents; successful empty list versus timeout; preserved timestamps/provider/model overrides; unknown session types; legacy-only fixture; pagination and large output bounds. Adapt `refresh_sessions_test.go`, `session_model_cache_test.go` if present, and existing `session_model_reader_test.go` coverage.

### 2. Cost and token accounting still depend entirely on JSONL

The collector only discovers `*/sessions/*.jsonl` and `*.jsonl.deleted.*`. It caches by file size and modification time and sums assistant usage, including input/output/cache reads/cache writes. There is no modern usage source in this path. After migration, historical files can continue producing plausible old totals while current SQLite-backed work is absent. With no readable files, empty buckets become zero totals and projections; unavailable data is not distinguished from measured zero. Daily cost alerts use those same buckets.

Evidence: [token_usage_cache.go:36](../../internal/apprefresh/token_usage_cache.go#L36), [token_usage_cache.go:150](../../internal/apprefresh/token_usage_cache.go#L150), [refresh.go:362](../../internal/apprefresh/refresh.go#L362), [refresh.go:413](../../internal/apprefresh/refresh.go#L413), [refresh_tokens.go:13](../../internal/apprefresh/refresh_tokens.go#L13).

Implementation seam: first verify the pinned runtime's supported usage-summary interface, then normalize it into existing `TokenBucket`/daily-series data. Preserve the project's zero third-party Go dependency constraint by using a supported OpenClaw JSON interface where available. If a matching interface is not available, show accounting as unavailable/partial rather than inventing costs or introducing an unreviewed direct SQLite reader. Add explicit coverage windows and source attribution so legacy and modern accounting cannot double-count overlapping data. Missing monetary cost must not silently mean a free request.

Acceptance coverage: current usage with no JSONL files; recorded zero versus missing cost; input/output/cache tokens; timezone boundaries; mixed migrated/legacy histories without duplicates; partial coverage; failed collection retaining a stale marked snapshot; alert suppression or qualification when accounting is incomplete.

### 3. Gateway status confuses service-manager state with live gateway state

`ParseGatewayStatusJSON` understands only `service.loaded`, `service.runtime.status/pid`, and top-level `version`. Any valid JSON without a running/loaded local service becomes `offline`. `CollectVersionsLocal` falls back to HTTP only when an error occurs and the parsed status remains `unknown`; the parser's `offline` result prevents this fallback. A healthy container/remote gateway can therefore be reported as offline while `/healthz` and `/readyz` correctly report it live and ready. The dashboard already has independent HTTP gateway-health support, so this is a consistency repair rather than a new health feature.

Evidence: [system_service.go:417](../../internal/appsystem/system_service.go#L417), [system_service.go:799](../../internal/appsystem/system_service.go#L799), [system_types.go:63](../../internal/appsystem/system_types.go#L63), [refresh_gateway.go:124](../../internal/apprefresh/refresh_gateway.go#L124).

Implementation seam: keep service installation state, RPC accessibility/authentication, liveness, readiness, and process metadata as separate fields. Let a documented gateway-health signal determine operational availability. Identify host CLI version and gateway runtime version separately. `ResolveOpenclawBin` chooses a PATH/asdf/brew binary; it does not establish that the binary is the gateway's runtime. Local `ps` must not be applied to an unqualified container PID.

Evidence for version/process ambiguity: [system_service.go:420](../../internal/appsystem/system_service.go#L420), [system_service.go:855](../../internal/appsystem/system_service.go#L855), [system_service.go:964](../../internal/appsystem/system_service.go#L964), [gateway_lock.go:88](../../internal/apprefresh/gateway_lock.go#L88).

Acceptance coverage: unmanaged/container gateway healthy with no host service; loaded service but failed health; healthy HTTP with pairing/auth failure; host and gateway version mismatch; unavailable container process metrics; no false host PID lookup. Extend `system_runtime_test.go`, `system_probes_test.go`, `system_service_test.go`, and `gateway_lock_test.go`.

### 4. Configuration projection misses active policy and memory fields

The inspected configuration has `memory.search.enabled=true`, provider `ollama`, an `agents.defaults.modelPolicy.allow` list, no explicit `agents.list`, and an object-valued gateway auth token. The dashboard's memory parser reads only `agents.defaults.memorySearch`; model parsing reads `defaults.models` and model fallbacks without examining `modelPolicy`. The fallback agent row is hardcoded as `id=default`, workspace `~/.openclaw/workspace`, even when the effective runtime agent and configured defaults workspace differ.

Evidence: selected-key inspection of `$HOME/.openclaw/openclaw.json` on the audit date; [refresh_config.go:62](../../internal/apprefresh/refresh_config.go#L62), [refresh_config.go:220](../../internal/apprefresh/refresh_config.go#L220), [refresh_config.go:482](../../internal/apprefresh/refresh_config.go#L482).

Other projection limits: plugin cards contain only configured entry names and enabled flags, ignoring the plugin allowlist and runtime availability. Each channel's account results are collapsed using boolean OR; one healthy account can make the channel look connected while another has failed. Direct `talk.apiKey` string presence is treated as TTS enablement, which is not a general runtime capability check. Raw JSON configuration reads also cannot resolve effective includes/JSON5/default inheritance.

Evidence: [refresh_config.go:113](../../internal/apprefresh/refresh_config.go#L113), [refresh_config.go:286](../../internal/apprefresh/refresh_config.go#L286), [refresh_config.go:369](../../internal/apprefresh/refresh_config.go#L369), [channel_status.go:58](../../internal/apprefresh/channel_status.go#L58), [refresh.go:330](../../internal/apprefresh/refresh.go#L330).

Implementation seam: normalize current and legacy fields at `parseOpenclawConfig`; use effective runtime/config information when available rather than inventing agent IDs and workspaces. Show model policy separately from catalog inventory. Add a memory configuration/health view and per-account channel drilldown. Project an explicit allowlist of public fields; do not serialize arbitrary configuration or resolved secret values.

Acceptance coverage: `memory.search` and legacy `memorySearch`; model policy distinct from inventory; implicit main agent and configured workspace; mixed account health; configured-but-blocked plugin; secret references treated as configured without exposing values. Extend `refresh_config_limits_test.go`, `refresh_config_unit_test.go`, `doc_alignment_test.go`, and channel-status fixtures.

### 5. Chat has a dotenv-only credential and one fixed transport

Startup reads `OPENCLAW_GATEWAY_TOKEN` only from the configured dotenv file. `ReadDotenv` does not overlay the process environment or resolve configuration SecretRefs. Missing dotenv credentials can terminate startup when AI is enabled. The HTTP request always targets `http://localhost:<port>/v1/chat/completions`, sends a bearer token, is non-streaming, and requests 512 output tokens. This does not prove chat is broken on the user's gateway; it establishes missing capability/auth negotiation. The inspected configuration has no explicit gateway HTTP endpoint block, so endpoint availability must be verified independently.

Evidence: [main.go:176](../../main.go#L176), [config.go:380](../../internal/appconfig/config.go#L380), [chat.go:277](../../internal/appchat/chat.go#L277), [server_chat_handler.go:100](../../internal/appserver/server_chat_handler.go#L100).

Implementation seam: capability-gate the chat panel, report the supported transport/auth state, and isolate token sourcing behind a server-side boundary. Retain only the required credential in memory and never expose it through dashboard data or browser code. Confirm the runtime's supported endpoint before selecting a transport. Streaming, persistent conversations, attachments, tool progress, and additional API transports are optional product work after basic compatibility is established.

Existing protections to preserve: bounded upstream response, request context and timeout classification, token redaction, safe generic client errors, bounded history. Relevant tests include `chat_redaction_test.go` and `server_chat_handler_test.go`.

### 6. Durable tasks are supported, but only a small subagent projection

The dashboard already calls `tasks list --json --runtime subagent`. It accepts an envelope or array and maps title/task, agent, status, duration, time, and error. It drops task identity, parent/session relationships, runtime kind, and other detail; the UI keys rows by truncated task text plus minute-level timestamp and renders only 20 rows. Distinct similarly titled tasks created in one minute can collide in row reconciliation. CLI failure is returned as `nil`, indistinguishable from no tasks.

Evidence: [subagent_tasks.go:15](../../internal/apprefresh/subagent_tasks.go#L15), [subagent_tasks.go:35](../../internal/apprefresh/subagent_tasks.go#L35), [subagent_tasks.go:87](../../internal/apprefresh/subagent_tasks.go#L87), [web/index.html:1460](../../web/index.html#L1460).

Implementation seam: retain stable task IDs and supported relationship fields, add source/error/freshness metadata, and generalize filtering to other runtime kinds only after validating 2026.8.2 payloads. Keep the current subagent view as a filter. Add pagination/details and render status values dynamically. Cost/token fields are intentionally omitted from task rows; do not populate them with guessed zeros. Usage may be joined only if a supported authoritative interface exposes a reliable relation.

Runtime cross-check from the coordinating investigation: the host dashboard returned five subagent rows while the running gateway's `tasks.list` RPC returned 21 total tasks; those scopes are not directly comparable. The pinned container's local tasks CLI failed to obtain a stable SQLite read snapshot. Therefore replacing every host subprocess with a container-local subprocess is not an adequate fix: verify each collector's data scope and choose its proven supported interface separately.

Acceptance coverage: duplicate titles in one minute; full identity preservation; unknown status/runtime enum; task failure versus empty task store; running duration versus finished duration; relationship fields; safe escaping. Existing fixtures and tests: `subagent_tasks_test.go`, `subagent_integration_test.go`, `testdata/cron/subagent-tasks.cli.json`.

### 7. Runtime data already exists that the UI barely exposes

The system API parses tasks, plugin compatibility, event loop, heartbeat, channel summary, agents, gateway and node services, memory, memory plugin, sessions, task audit, security audit, secret diagnostics, and update metadata. The runtime card currently renders task summary/by-status, event-loop values, plugin warning count, heartbeat, and channel summary. Expanding this existing view is smaller than building duplicate collectors. Its task status renderer uses a fixed enum list even though the backend maps support future statuses; runtime kind counts are not shown.

Evidence: [system_types.go:95](../../internal/appsystem/system_types.go#L95), [system_service.go:617](../../internal/appsystem/system_service.go#L617), [web/index.html:2210](../../web/index.html#L2210).

Implementation seam: add concise memory/index health, plugin warning detail, task runtime/status detail, authentication/RPC diagnostics, and update/runtime identity rows from existing normalized fields. Add specialized probes only for data missing from the status contract. Review redaction before rendering presently unused diagnostics. Preserve lean/deep status separation and source freshness.

### 8. Logs need deployment-aware source discovery

The dashboard supports local files and journald, with automatic fallback to macOS `~/Library/Logs/openclaw`. The fallback mechanism derives the same basename under those roots; it does not establish a live container log source or discover arbitrary timestamped runtime logs. Container stdout or `/tmp` logs require a supported explicit source/bridge or mounted files. Historical host logs should not be silently presented as current container logs.

Evidence: [logtail.go:583](../../internal/apprefresh/logtail.go#L583), [logtail.go:604](../../internal/apprefresh/logtail.go#L604), [logtail_journald.go](../../internal/apprefresh/logtail_journald.go), [config.go:142](../../internal/appconfig/config.go#L142).

Implementation seam: expose each log source's deployment, resolved location, last observed timestamp, and availability; then select a bounded runtime-supported log source. Retain local-file/journald compatibility and merged rotation handling. Acceptance fixtures should distinguish rotated files, stale host files, current container stream, source failure, and timestamp/size bounds.

### 9. Latest-version lookup truncates the current npm metadata response

`FetchLatestNpmVersion` reads the complete `/openclaw/latest` package metadata document through the same 64 KiB limiter used for gateway JSON. The coordinating investigation measured the live document at 105,475 bytes and found repeated `unexpected EOF` errors in the dashboard log. The source confirms that a truncated response returns an empty version string. This prevents reliable update comparison independently of the gateway upgrade.

Evidence: [system_service.go:77](../../internal/appsystem/system_service.go#L77), [system_service.go:1005](../../internal/appsystem/system_service.go#L1005), [system_service.go:1029](../../internal/appsystem/system_service.go#L1029); response-size/log measurements from the coordinating live investigation.

Implementation seam: verify and use the small npm dist-tags response for the latest tag, or give this specific endpoint an adequate independent bound. Keep explicit size limits and timeout behavior; do not increase all gateway response limits indiscriminately. Acceptance coverage: valid metadata exceeding 64 KiB, oversized invalid response, unavailable registry, cached last known value, and explicit unknown latest version.

## Already implemented: avoid duplicate roadmap items

| Capability | Existing support | Remaining work |
|---|---|---|
| Cron migration | CLI preferred; legacy jobs/state fallback; schedule/state mapping | Prove runtime routing; freshness/error state and deeper history if required |
| Durable subagents | Tasks CLI, title/status/agent/duration | Stable IDs, broader task view, detail and error state |
| Model changes | Live catalog names/windows, aliases, string/object model config, CLI live session models | Authoritative session enumeration; effective model policy |
| Channels | CLI account probes overlaid on config; readiness failures override activity heuristic | Preserve account detail and distinguish degraded aggregate health |
| Runtime | HTTP liveness/readiness, lean/deep status, task/event-loop/plugin/heartbeat card | Reconcile service versus gateway state; expose existing rich blocks |
| Logs | Tail bounds, merged rotation/location handling, journald support | Identify current deployment source and stale source state |
| Chat | Bounded requests/responses, context, token redaction, generic client errors | Credential/capability resolution, then optional richer interaction |

Evidence: [refresh.go:305](../../internal/apprefresh/refresh.go#L305), [refresh_crons.go:79](../../internal/apprefresh/refresh_crons.go#L79), [channel_status.go:12](../../internal/apprefresh/channel_status.go#L12), [refresh_sessions.go:381](../../internal/apprefresh/refresh_sessions.go#L381), [model_catalog_cache.go:74](../../internal/apprefresh/model_catalog_cache.go#L74), plus the detailed findings above.

## Suggested implementation order and verification gates

1. **Establish one runtime target and honest collection state.** Distinguish host CLI/gateway versions, service state/RPC/HTTP, and successful-empty/failed/stale/unsupported collectors. Verify each collector's interface and scope with pinned-version fixtures and harmless live status checks. Repair the independently confirmed latest-version response truncation.
2. **Restore authoritative sessions and accounting.** Implement session snapshot collection first, then supported usage summaries with coverage/source identity. Gate on SQLite-era fixtures without legacy files and migration deduplication tests.
3. **Correct effective configuration and enrich existing runtime UI.** Add memory/model-policy/effective-agent/account/plugin projections, preserving secret boundaries. Gate on the user's sanitized current configuration and legacy compatibility fixtures.
4. **Extend task, cron, and log visibility.** Stable IDs, runtime filters, detail/history, failure state, and deployed log sources. Gate on duplicate tasks, pagination, mixed account/runtime status, and bounded source reads.
5. **Add optional interaction capabilities.** Chat transport/capability work, task operations or config editing require explicit product scope and verified upstream contracts; the audit itself does not authorize those operations.

Keep logic in `internal/apprefresh`, `internal/appsystem`, `internal/appchat`, or `internal/appconfig`; root wrappers remain a facade. Preserve the embedded single-file SPA and rebuild for UI changes. Use repository targets: `make test` already runs race tests; `make check` includes vet/lint/tests/vulnerability/static checks/build. Add meaningful contract and acceptance coverage for changed behavior, then run required repo checks. Do not add a SQLite Go dependency merely to bypass source adaptation.
