// Package apprefresh collects dashboard data including sessions, tokens, crons, and gateway health.
package apprefresh

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// RunRefreshCollector generates data.json from the selected OpenClaw runtime
// or legacy native filesystem data.
// Callers must supply the active dashboard Config; use appconfig.Load(dir) at
// the call site if no Config is on hand.
func RunRefreshCollector(ctx context.Context, dashboardDir, openclawPath string, cfg appconfig.Config) error {
	if err := cfg.Openclaw.Validate(); err != nil {
		return err
	}
	data := collectDashboardData(ctx, dashboardDir, openclawPath, cfg)
	retainLastGoodCollections(data, readPreviousSnapshot(filepath.Join(dashboardDir, "data.json")))

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data.json: %w", err)
	}

	finalPath := filepath.Join(dashboardDir, "data.json")
	if err := writeSnapshotAtomic(finalPath, out); err != nil {
		return fmt.Errorf("write data.json: %w", err)
	}
	return nil
}

// writeSnapshotAtomic gives each publisher its own 0600 temporary inode. A
// separate --refresh process must never keep writing into our published file.
func writeSnapshotAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const (
	// modelCatalogTTL bounds how long the live model display-name catalog is
	// reused before refreshModelCatalog queries the runtime again.
	modelCatalogTTL = 5 * time.Minute
	// projectionDaysPerMonth scales today's cost to the projected monthly cost.
	projectionDaysPerMonth = 30
)

var reStripTelegramID = regexp.MustCompile(`(?i)\s*\bid\b[\s:=\-]*\d+`)

// TitleCase uppercases the first byte of an ASCII string. Replaces the
// deprecated strings.Title for simple provider-name normalization.
func TitleCase(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// TrimLabel strips trailing/embedded "id:NNN" annotations (telegram IDs etc.)
// from a label. Case-insensitive; tolerant of `=`, `-`, space, and combinations.
func TrimLabel(s string) string {
	if s == "" {
		return s
	}
	return strings.TrimSpace(reStripTelegramID.ReplaceAllString(s, ""))
}

// catalogNameIsBareID reports whether a catalog display name is just the
// model's bare id (the segment after the last "/"), case-insensitively — i.e.
// openclaw registered no real display name for it.
func catalogNameIsBareID(model, name string) bool {
	bare := model
	if i := strings.LastIndex(model, "/"); i >= 0 {
		bare = model[i+1:]
	}
	return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(bare))
}

// upperFamily renders a bare family id like "glm-5.2" or "gpt-5.5" as
// "GLM-5.2" / "GPT-5.5", uppercasing the family token while preserving the full
// numeric version that a flat family label (e.g. "GLM-5") would otherwise drop.
// Only the leading "-<digits and dots>" version is kept; tier/variant suffixes
// (e.g. "-plus", "-air") collapse to the versioned family as before. id is the
// lower-cased model segment; token is the family as it appears in id (e.g.
// "glm"); prefix is its canonical upper-case label (e.g. "GLM").
func upperFamily(id, token, prefix string) string {
	_, after, ok := strings.Cut(id, token)
	if !ok {
		return prefix
	}
	rest := strings.TrimPrefix(after, "-")
	end := 0
	for end < len(rest) && (rest[end] == '.' || (rest[end] >= '0' && rest[end] <= '9')) {
		end++
	}
	if end == 0 {
		return prefix
	}
	return prefix + "-" + rest[:end]
}

// claudeFamilyName renders an Anthropic id like "claude-opus-4-6" as
// "Claude Opus 4.6", reading the dashed version after the family token (4-6 →
// 4.6). A dash only continues the version when a digit follows, so a word suffix
// (e.g. "-thinking") stops it. With no version it returns "Claude <family>".
// token is the lower-case family as it appears in the id ("opus"); family is the
// display label ("Opus").
func claudeFamilyName(id, token, family string) string {
	_, after, ok := strings.Cut(id, token)
	if !ok {
		return "Claude " + family
	}
	rest := strings.TrimPrefix(after, "-")
	if rest == "" {
		return "Claude " + family
	}
	// Version parts are short numeric segments joined by dashes ("4-6" → 4.6).
	// Stop at a long numeric segment (a YYYYMMDD date snapshot) or any
	// non-numeric suffix (e.g. "-thinking").
	var parts []string
	for seg := range strings.SplitSeq(rest, "-") {
		if len(seg) == 0 || len(seg) > 3 || !isAllDigits(seg) {
			break
		}
		parts = append(parts, seg)
	}
	if len(parts) == 0 {
		return "Claude " + family
	}
	return "Claude " + family + " " + strings.Join(parts, ".")
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ModelName returns the human-friendly display name for a model identifier.
// Falls back to the raw id when no friendly name is known.
func ModelName(model string) string {
	// Prefer the live catalog name, but ignore a catalog "name" that is merely
	// the bare model id (openclaw's fallback when no friendly name is
	// registered) — it is no better than the raw id and must not shadow a
	// curated display name below.
	if name, ok := catalogDisplayName(model); ok && !catalogNameIsBareID(model, name) {
		return name
	}
	ml := strings.ToLower(model)
	if strings.Contains(ml, "/") {
		parts := strings.SplitN(ml, "/", 2)
		ml = parts[1]
	}
	// Segment-anchored boundary check for OpenAI o1/o3 reasoning models so
	// arbitrary substrings like "gpt-fo1bar" or "o1foo" do not trip the O1
	// rule. Tokens are split on common separators (`/`, `-`, `:`, `.`, `_`)
	// and the target must appear as a standalone segment.
	segments := strings.FieldsFunc(ml, func(r rune) bool {
		return r == '/' || r == '-' || r == ':' || r == '.' || r == '_'
	})
	hasOSegment := func(seg string) bool {
		return slices.Contains(segments, seg)
	}
	switch {
	case strings.Contains(ml, "opus"):
		return claudeFamilyName(ml, "opus", "Opus")
	case strings.Contains(ml, "sonnet"):
		return claudeFamilyName(ml, "sonnet", "Sonnet")
	case strings.Contains(ml, "haiku"):
		return claudeFamilyName(ml, "haiku", "Haiku")
	case strings.Contains(ml, "grok-4-fast"):
		return "Grok 4 Fast"
	case strings.Contains(ml, "grok-4") || strings.Contains(ml, "grok4"):
		return "Grok 4"
	case strings.Contains(ml, "gemini-2.5-pro") || strings.Contains(ml, "gemini-pro"):
		return "Gemini 2.5 Pro"
	case strings.Contains(ml, "gemini-3-flash"):
		return "Gemini 3 Flash"
	case strings.Contains(ml, "gemini-2.5-flash"):
		return "Gemini 2.5 Flash"
	case strings.Contains(ml, "flash"):
		return "Gemini Flash"
	case strings.Contains(ml, "gemini"):
		// Tier-neutral: an unrecognized Gemini (Pro/Ultra/future) must not be
		// mislabeled "Flash". A genuine flash id is caught by the case above.
		return "Gemini"
	case strings.Contains(ml, "minimax-m2.5"):
		return "MiniMax M2.5"
	case strings.Contains(ml, "minimax-m2") || strings.Contains(ml, "minimax"):
		return "MiniMax"
	case strings.Contains(ml, "glm-5"):
		return upperFamily(ml, "glm", "GLM")
	case strings.Contains(ml, "glm-4"):
		return upperFamily(ml, "glm", "GLM")
	case strings.Contains(ml, "k2p5"):
		return "Kimi K2.5"
	case strings.Contains(ml, "kimi"):
		// Tier-neutral: an unrecognized Kimi id must not be pinned to "K2.5".
		return "Kimi"
	case hasOSegment("o1"):
		return "O1"
	case hasOSegment("o3"):
		return "O3"
	case strings.Contains(ml, "gpt-5.3-codex"):
		return "GPT-5.3 Codex"
	case strings.Contains(ml, "gpt-5"):
		return upperFamily(ml, "gpt", "GPT")
	case strings.Contains(ml, "gpt-4o"):
		return "GPT-4o"
	case strings.Contains(ml, "gpt-4"):
		return upperFamily(ml, "gpt", "GPT")
	default:
		return model
	}
}

// collectDashboardData orchestrates parallel data collectors and assembles the
// dashboard JSON payload. Most work is delegated to refresh_*.go siblings.
func collectDashboardData(ctx context.Context, dashboardDir, openclawPath string, cfg appconfig.Config) map[string]any {
	ctx = appopenclaw.WithTarget(ctx, cfg.Openclaw)
	openclawPath = cfg.Openclaw.StatePath(openclawPath)
	window := newRefreshWindow(cfg.Timezone, time.Now())
	now, loc := window.now, window.loc
	todayStr, date7d, date30d := window.today, window.date7d, window.date30d

	basePath := filepath.Join(openclawPath, "agents")
	configPath := filepath.Join(openclawPath, "openclaw.json")
	cronPath := filepath.Join(openclawPath, "cron/jobs.json")
	modern := hasRuntimeState(basePath, cfg.Openclaw)
	client := appopenclaw.Client{Binary: resolveOpenclawBin(), Runner: execCommandContext}
	collections := map[string]CollectionStatus{}

	// Bot config and alert thresholds: zero values fall back to appconfig's
	// defaults so a partially populated Config matches a loaded one.
	defaults := appconfig.Default()
	botName := cmp.Or(cfg.Bot.Name, defaults.Bot.Name)
	botEmoji := cmp.Or(cfg.Bot.Emoji, defaults.Bot.Emoji)
	costThresholdHigh := cmp.Or(cfg.Alerts.DailyCostHigh, defaults.Alerts.DailyCostHigh)
	costThresholdWarn := cmp.Or(cfg.Alerts.DailyCostWarn, defaults.Alerts.DailyCostWarn)
	contextThreshold := cmp.Or(cfg.Alerts.ContextPct, defaults.Alerts.ContextPct)
	memoryThresholdKB := cmp.Or(cfg.Alerts.MemoryMb, defaults.Alerts.MemoryMb) * 1024

	// Refresh the live model display-name catalog (TTL-cached) BEFORE the
	// collectors so ModelName resolves current model ids for crons — collected in
	// the goroutine below — as well as sessions and token usage downstream.
	refreshModelCatalog(ctx, now, modelCatalogTTL)

	// Kick off independent collectors concurrently.
	var gateway map[string]any
	var crons []map[string]any
	var gitLog []map[string]any
	var cronErr error
	var cwg sync.WaitGroup
	cwg.Add(3)
	go func() {
		defer cwg.Done()
		gateway = collectGatewayHealthWithLock(ctx, openclawPath, cfg.AI.GatewayPort)
	}()
	go func() {
		defer cwg.Done()
		if modern {
			crons, cronErr = collectRuntimeCrons(ctx, client, loc)
			return
		}
		// OpenClaw 2026.6+ moved cron jobs into shared SQLite, served via the
		// gateway CLI. Prefer it; fall back to the legacy jobs.json file for
		// pre-migration installs (or when the gateway is unavailable).
		crons, cronErr = collectCronsSnapshot(ctx, nil, nil, loc)
		if cronErr != nil {
			crons = CollectCrons(cronPath, loc)
		}
	}()
	go func() {
		defer cwg.Done()
		if !cfg.Openclaw.IsContainer() {
			gitLog = collectGitLog(ctx, openclawPath)
		}
	}()

	// The legacy CLI and probe reads depend on nothing collected below, so
	// they start now and overlap the collectors above instead of running one
	// after another later in the pass. lwg is waited before their first use.
	sessionLiveModelTTL := time.Duration(cfg.Refresh.IntervalSeconds) * time.Second
	probeReadyz := !modern && !appopenclaw.TargetFromContext(ctx).IsContainer()
	var lwg sync.WaitGroup
	var cliChannelStatus map[string]any
	var cliChannelStatusOK bool
	var readyzFailing []string
	var legacySubagentRuns []map[string]any
	if !modern {
		// Warms the cache collectSessions reads; a lookup made while this
		// fetch is in flight waits for it rather than starting another.
		lwg.Go(func() { getLiveSessionModels(ctx, now, sessionLiveModelTTL) })
		lwg.Go(func() { cliChannelStatus, cliChannelStatusOK = channelStatusCollector(ctx, nil, nil) })
		lwg.Go(func() { legacySubagentRuns = collectSubagentRuns(ctx, nil, nil, loc) })
	}
	if probeReadyz {
		lwg.Go(func() { readyzFailing, _ = readyzProbe(ctx, cfg.AI.GatewayPort) })
	}

	// Read configuration from the selected runtime while collectors are in flight.
	compactionMode := "unknown"
	var skills []map[string]any
	var availableModels []map[string]any
	modelAliases := map[string]string{}
	agentConfig := defaultAgentConfig()

	oc, selectedConfigPath, configErr := readSelectedConfig(ctx, client, configPath)
	configSource := "native.config.file"
	if cfg.Openclaw.IsContainer() {
		configSource = "gateway.config.get"
	}
	collections["configuration"] = collectionStatus(configSource, configErr, configErr == nil)
	if configErr == nil {
		compactionMode, skills, availableModels, modelAliases, agentConfig =
			parseOpenclawConfig(oc, filepath.Join(filepath.Dir(selectedConfigPath), "agents"))
	}

	var sessionStores []SessionStoreFile
	if !cfg.Openclaw.IsContainer() {
		sessionStores = loadSessionStores(basePath)
	}
	rt := newRuntimeCollection(window)
	if modern {
		rt.start(ctx, &cwg, client, runtimeAgentIDs(agentConfig), loc, modelAliases, todayStr)
	}

	// Build group names from session data for bindings
	groupNames := buildGroupNames(sessionStores)
	enrichBindings(agentConfig, groupNames)

	// Wait for gateway before building sessions (sessions need gateway map)
	cwg.Wait()
	runtimeSessions, tasks := rt.sessions, rt.tasks
	collections["crons"] = collectionStatus("cli.cron.list", cronErr, cronErr == nil)
	if modern {
		collections["crons"] = collectionStatus("gateway.cron.list", cronErr, cronErr == nil)
		if errors.Is(cronErr, errCronRowLimit) {
			collections["crons"] = partialCollectionStatus("gateway.cron.list", "row_limit")
		}
	}
	if cronErr != nil && !modern && crons != nil {
		// The file rows are real, but the authoritative CLI never answered, so
		// the snapshot may be arbitrarily out of date. Report the fallback and
		// the failure rather than a healthy collection.
		collections["crons"] = partialCollectionStatus("legacy.cron.files", appopenclaw.ErrorCode(cronErr))
	}

	// Sessions
	knownSIDs := map[string]string{}
	var sessionsList []map[string]any
	if modern {
		sessionsList = runtimeSessions.Rows
		collections["sessions"] = collectionStatus("gateway.sessions.list", rt.sessionErr, runtimeSessions.Complete)
		for _, row := range sessionsList {
			if sid := jsonStr(row, "sessionId"); sid != "" {
				knownSIDs[sid] = jsonStr(row, "type")
			}
		}
		collections["tasks"] = collectionStatus("gateway.tasks.list", rt.taskErr, rt.taskErr == nil)
		if errors.Is(rt.taskErr, errTaskRowLimit) {
			// Truncation is not an outage: keep the collected rows visible and
			// say the page walk stopped short, matching the sessions collector.
			collections["tasks"] = partialCollectionStatus("gateway.tasks.list", "row_limit")
		}
	} else {
		sessionsList = collectSessions(ctx, sessionStores, basePath, loc, now, modelAliases, knownSIDs, sessionLiveModelTTL)
		collections["sessions"] = collectionStatus("legacy.session.files", nil, true)
	}

	lwg.Wait()
	if cliChannelStatusOK {
		overlayChannelStatus(agentConfig, cliChannelStatus)
	}

	// Backfill channel connectivity: gateway /readyz failing[] is authoritative
	// for failures; on probe failure we fall back to the session-activity
	// heuristic (failing is nil, so no channel is blanked).
	if probeReadyz {
		backfillChannelConnectivity(agentConfig, sessionsList, readyzFailing)
	} else {
		agentConfig["channelStatus"] = map[string]any{}
	}

	// Token usage from JSONL. CollectTokenUsageWithCache drives the main
	// per-model token usage and cost buckets (the primary Token Usage panel).
	// Its subagent-run output is now empty — OpenClaw 2026.6+ moved subagent
	// tracking out of session keys — so its return is discarded; subagent runs
	// come from the durable tasks store.
	u := newLegacyUsageBuckets()
	if !modern {
		u.collect(filepath.Join(dashboardDir, ".token-usage-cache.json"), basePath, window,
			knownSIDs, BuildSIDToKeyMap(sessionStores), modelAliases)
	}

	// OpenClaw 2026.6+ tracks subagent runs as durable tasks in shared SQLite,
	// served via the gateway CLI. Cost/token data is unavailable there, so runs
	// carry status/duration/agent metadata only.
	var subagentRuns []map[string]any
	if modern {
		for _, task := range tasks {
			if task["runtime"] == "subagent" {
				subagentRuns = append(subagentRuns, task)
			}
		}
	} else {
		subagentRuns = legacySubagentRuns
	}

	slices.SortFunc(subagentRuns, func(a, b map[string]any) int {
		ta, _ := a["timestamp"].(string)
		tb, _ := b["timestamp"].(string)
		return cmp.Compare(tb, ta)
	})

	subagentRunsToday := FilterByDate(subagentRuns, todayStr, "==")
	subagentRuns7d := FilterByDate(subagentRuns, date7d, ">=")
	subagentRuns30d := FilterByDate(subagentRuns, date30d, ">=")

	// Build daily chart data (last 30 days)
	dailyChart := BuildDailyChart(now, u.dailyCosts, u.dailyTokens, u.dailyCalls,
		u.dailySubagentCosts, u.dailySubagentCount, dashboardDir)

	// Alerts
	totalCostToday := sumBucketCosts(u.modelsToday)
	totalCostAll := sumBucketCosts(u.modelsAll)

	alertCostToday := totalCostToday
	if modern {
		// Modern collectors do not populate legacy token buckets. Alert only on
		// a complete current total, never an incomplete pricing subtotal.
		for _, r := range rt.usage {
			if r.suffix == "Today" && r.err == nil && r.result.CompleteCost() {
				alertCostToday = *r.result.Totals.TotalCost
			}
		}
	}
	alertSessions := sessionsList
	if modern {
		alertSessions = alertableRuntimeSessions(sessionsList, now)
	}
	alerts := BuildAlerts(alertCostToday, costThresholdHigh, costThresholdWarn,
		crons, alertSessions, contextThreshold, gateway, memoryThresholdKB)

	// Cost breakdown
	costBreakdown := BuildCostBreakdown(u.modelsAll)
	costBreakdownToday := BuildCostBreakdown(u.modelsToday)

	// Projected monthly
	projectedMonthly := totalCostToday * projectionDaysPerMonth

	data := map[string]any{
		"schemaVersion": 2,
		"timezone":      loc.String(),
		"runtimeTarget": cfg.Openclaw.Effective(),
		"stateDir":      openclawPath,
		"collections":   collections,
		"tasks":         tasks,
		"sessionTotal":  runtimeSessions.Total,
		"botName":       botName,
		"botEmoji":      botEmoji,
		"lastRefresh":   now.Format("2006-01-02 15:04:05 ") + loc.String(),
		"lastRefreshMs": now.UnixMilli(),

		"gateway":        gateway,
		"compactionMode": compactionMode,

		"totalCostToday":     round2(totalCostToday),
		"totalCostAllTime":   round2(totalCostAll),
		"projectedMonthly":   round2(projectedMonthly),
		"costBreakdown":      costBreakdown,
		"costBreakdownToday": costBreakdownToday,

		"sessions":     LimitSlice(sessionsList, 20),
		"sessionCount": len(knownSIDs),

		"crons": crons,

		"subagentRuns":        LimitSlice(subagentRuns, 30),
		"subagentRunsToday":   LimitSlice(subagentRunsToday, 20),
		"subagentRuns7d":      LimitSlice(subagentRuns7d, 50),
		"subagentRuns30d":     LimitSlice(subagentRuns30d, 100),
		"subagentCostAllTime": round2(sumBucketCosts(u.subagentAll)),
		"subagentCostToday":   round2(sumBucketCosts(u.subagentToday)),
		"subagentCost7d":      round2(sumBucketCosts(u.subagent7d)),
		"subagentCost30d":     round2(sumBucketCosts(u.subagent30d)),

		"tokenUsage":         BucketsToList(u.modelsAll),
		"tokenUsageToday":    BucketsToList(u.modelsToday),
		"tokenUsage7d":       BucketsToList(u.models7d),
		"tokenUsage30d":      BucketsToList(u.models30d),
		"subagentUsage":      BucketsToList(u.subagentAll),
		"subagentUsageToday": BucketsToList(u.subagentToday),
		"subagentUsage7d":    BucketsToList(u.subagent7d),
		"subagentUsage30d":   BucketsToList(u.subagent30d),

		"dailyChart": dailyChart,
		"logConfig":  GetLogRuntimeConfig(cfg),

		"availableModels": availableModels,
		"agentConfig":     agentConfig,
		"skills":          skills,

		"gitLog": gitLog,
		"alerts": alerts,
	}
	data["sessionTotal"] = data["sessionCount"]
	if modern {
		rt.applyTo(data, collections, configErr != nil, now)
	}
	return data
}

// refreshWindow is the clock of one collection pass: the configured zone and
// the calendar-day boundaries derived from it.
type refreshWindow struct {
	now                    time.Time
	loc                    *time.Location
	today, date7d, date30d string
}

// newRefreshWindow resolves timezone (empty means UTC; unknown falls back to
// UTC with a warning) and derives the day windows from now in that zone.
func newRefreshWindow(timezone string, now time.Time) refreshWindow {
	tzName := cmp.Or(timezone, "UTC")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
		fmt.Fprintf(os.Stderr, "[dashboard warn] Unknown timezone '%s', using UTC\n", tzName)
	}
	now = now.In(loc)
	// Windows are inclusive of today in the configured timezone: "7d" spans
	// today + 6 prior days and "30d" today + 29 prior — matching the 30-bucket
	// daily chart (refresh_chart.go counts i := 29; i >= 0) and today, all of
	// which derive from the loc-zoned now. These must be computed AFTER
	// now.In(loc); doing so earlier skews the window by a day whenever the host
	// zone differs from cfg.Timezone across a midnight boundary.
	return refreshWindow{
		now:     now,
		loc:     loc,
		today:   now.Format("2006-01-02"),
		date7d:  now.AddDate(0, 0, -6).Format("2006-01-02"),
		date30d: now.AddDate(0, 0, -29).Format("2006-01-02"),
	}
}

// runtimeUsageRange is one sessions.usage window collected for the payload.
type runtimeUsageRange struct {
	suffix, period, start string
	result                runtimeUsage
	err                   error
}

// runtimeCollection holds the results of the migrated-runtime collectors that
// run concurrently with the rest of collectDashboardData. Fields are written
// by start's goroutines and must only be read after the WaitGroup is done.
type runtimeCollection struct {
	sessions             sessionSnapshot
	sessionErr           error
	tasks                []map[string]any
	taskErr              error
	health               map[string]any
	healthStatuses       map[string]CollectionStatus
	inventories          map[string]any
	inventoryStatuses    map[string]CollectionStatus
	providerStatus       map[string]any
	providerStatuses     map[string]CollectionStatus
	modelReadiness       []map[string]any
	modelReadinessStatus CollectionStatus
	usage                []runtimeUsageRange
}

func newRuntimeCollection(window refreshWindow) *runtimeCollection {
	return &runtimeCollection{usage: []runtimeUsageRange{
		{suffix: "All", period: "all"},
		{suffix: "Today", start: window.today},
		{suffix: "7d", start: window.date7d},
		{suffix: "30d", start: window.date30d},
	}}
}

// runtimeAgentIDs lists the configured agent ids, defaulting to ["main"].
func runtimeAgentIDs(agentConfig map[string]any) []string {
	agentIDs := []string{}
	for _, a := range agentConfig["agents"].([]any) {
		if id := jsonStr(asObj(a), "id"); id != "" {
			agentIDs = append(agentIDs, id)
		}
	}
	if len(agentIDs) == 0 {
		agentIDs = []string{"main"}
	}
	return agentIDs
}

// start launches every runtime collector on wg.
func (rt *runtimeCollection) start(ctx context.Context, wg *sync.WaitGroup, client appopenclaw.Client,
	agentIDs []string, loc *time.Location, modelAliases map[string]string, today string) {
	wg.Go(func() { rt.providerStatus, rt.providerStatuses = collectRuntimeProviderStatus(ctx, client) })
	wg.Go(func() { rt.modelReadiness, rt.modelReadinessStatus = collectRuntimeModels(ctx, client, agentIDs) })
	wg.Go(func() { rt.health, rt.healthStatuses = collectRuntimeHealth(ctx, client, agentIDs) })
	wg.Go(func() { rt.inventories, rt.inventoryStatuses = collectRuntimeInventories(ctx, client) })
	wg.Go(func() { rt.sessions, rt.sessionErr = collectRuntimeSessions(ctx, client, loc, modelAliases) })
	wg.Go(func() { rt.tasks, rt.taskErr = collectRuntimeTasks(ctx, client, loc) })
	for i := range rt.usage {
		wg.Go(func() {
			r := &rt.usage[i]
			r.result, r.err = collectRuntimeUsage(ctx, client, r.period, r.start, today, loc.String())
		})
	}
}

// applyTo overlays the runtime results onto the assembled payload, replacing
// the legacy-only session, usage, and subagent-cost fields.
func (rt *runtimeCollection) applyTo(data map[string]any, collections map[string]CollectionStatus, configFailed bool, now time.Time) {
	data["sessions"] = rt.sessions.Rows
	data["sessionCount"] = rt.sessions.Total
	data["sessionTotal"] = rt.sessions.Total
	data["modelReadiness"] = rt.modelReadiness
	collections["modelReadiness"] = rt.modelReadinessStatus
	maps.Copy(data, rt.providerStatus)
	maps.Copy(collections, rt.providerStatuses)
	maps.Copy(data, rt.health)
	maps.Copy(data, rt.inventories)
	maps.Copy(collections, rt.healthStatuses)
	maps.Copy(collections, rt.inventoryStatuses)
	if configFailed {
		// Without configuration the agent roster is the guessed ["main"],
		// so these per-agent collections cover an unknown fraction of the
		// runtime. Keep the rows, drop the claim that they are complete.
		for _, name := range []string{"memory", "skillInventory", "modelReadiness"} {
			if status, ok := collections[name]; ok && status.State == "ready" {
				collections[name] = partialCollectionStatus(status.Source, "configuration_unavailable")
			}
		}
	}
	for _, r := range rt.usage {
		collections["usage"+r.suffix] = collectionStatus("gateway.sessions.usage", r.err, r.result.CompleteTokens())
		applyRuntimeUsage(data, r.suffix, r.result, now)
	}
	for _, field := range []string{"subagentCostAllTime", "subagentCostToday", "subagentCost7d", "subagentCost30d"} {
		data[field] = nil
	}
}

// legacyUsageBuckets are the token and cost aggregates filled from legacy
// JSONL transcripts; on migrated runtimes they stay empty.
type legacyUsageBuckets struct {
	modelsAll, modelsToday, models7d, models30d         map[string]*TokenBucket
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*TokenBucket
	dailyCosts                                          map[string]map[string]float64
	dailyTokens, dailyCalls                             map[string]map[string]int
	dailySubagentCosts                                  map[string]float64
	dailySubagentCount                                  map[string]int
}

func newLegacyUsageBuckets() *legacyUsageBuckets {
	return &legacyUsageBuckets{
		modelsAll:          map[string]*TokenBucket{},
		modelsToday:        map[string]*TokenBucket{},
		models7d:           map[string]*TokenBucket{},
		models30d:          map[string]*TokenBucket{},
		subagentAll:        map[string]*TokenBucket{},
		subagentToday:      map[string]*TokenBucket{},
		subagent7d:         map[string]*TokenBucket{},
		subagent30d:        map[string]*TokenBucket{},
		dailyCosts:         map[string]map[string]float64{},
		dailyTokens:        map[string]map[string]int{},
		dailyCalls:         map[string]map[string]int{},
		dailySubagentCosts: map[string]float64{},
		dailySubagentCount: map[string]int{},
	}
}

// collect aggregates the transcripts under basePath into u via the on-disk
// token usage cache at cachePath.
func (u *legacyUsageBuckets) collect(cachePath, basePath string, window refreshWindow,
	knownSIDs, sidToKey, modelAliases map[string]string) {
	CollectTokenUsageWithCache(
		cachePath,
		basePath, window.loc, window.today, window.date7d, window.date30d,
		knownSIDs, sidToKey, modelAliases,
		u.modelsAll, u.modelsToday, u.models7d, u.models30d,
		u.subagentAll, u.subagentToday, u.subagent7d, u.subagent30d,
		u.dailyCosts, u.dailyTokens, u.dailyCalls, u.dailySubagentCosts, u.dailySubagentCount,
	)
}
