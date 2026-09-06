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

// ModelName returns the human-friendly display name for a model identifier.
// Falls back to the raw id when no friendly name is known.
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
	now := time.Now()
	tzName := cfg.Timezone
	if tzName == "" {
		tzName = "UTC"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
		fmt.Fprintf(os.Stderr, "[dashboard warn] Unknown timezone '%s', using UTC\n", tzName)
	}
	now = now.In(loc)
	todayStr := now.Format("2006-01-02")
	// Windows are inclusive of today in the configured timezone: "7d" spans
	// today + 6 prior days and "30d" today + 29 prior — matching the 30-bucket
	// daily chart (refresh_chart.go counts i := 29; i >= 0) and todayStr, all of
	// which derive from the loc-zoned now. These must be computed AFTER
	// now.In(loc); doing so earlier skews the window by a day whenever the host
	// zone differs from cfg.Timezone across a midnight boundary.
	date7d := now.AddDate(0, 0, -6).Format("2006-01-02")
	date30d := now.AddDate(0, 0, -29).Format("2006-01-02")

	basePath := filepath.Join(openclawPath, "agents")
	configPath := filepath.Join(openclawPath, "openclaw.json")
	cronPath := filepath.Join(openclawPath, "cron/jobs.json")
	modern := hasRuntimeState(basePath, cfg.Openclaw)
	client := appopenclaw.Client{Binary: resolveOpenclawBin(), Runner: execCommandContext}
	collections := map[string]CollectionStatus{}

	// Bot config
	botName := cfg.Bot.Name
	if botName == "" {
		botName = "OpenClaw Dashboard"
	}
	botEmoji := cfg.Bot.Emoji
	if botEmoji == "" {
		botEmoji = "⚡"
	}

	// Alert thresholds
	costThresholdHigh := cfg.Alerts.DailyCostHigh
	if costThresholdHigh == 0 {
		costThresholdHigh = 50
	}
	costThresholdWarn := cfg.Alerts.DailyCostWarn
	if costThresholdWarn == 0 {
		costThresholdWarn = 20
	}
	contextThreshold := cfg.Alerts.ContextPct
	if contextThreshold == 0 {
		contextThreshold = 80
	}
	memoryThresholdKB := cfg.Alerts.MemoryMb * 1024
	if memoryThresholdKB == 0 {
		memoryThresholdKB = 640 * 1024
	}

	// Refresh the live model display-name catalog (TTL-cached) BEFORE the
	// collectors so ModelName resolves current model ids for crons — collected in
	// the goroutine below — as well as sessions and token usage downstream.
	refreshModelCatalog(ctx, now, 5*time.Minute)

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
		if cronErr != nil && !modern {
			crons = CollectCrons(cronPath, loc)
		}
	}()
	go func() {
		defer cwg.Done()
		if !cfg.Openclaw.IsContainer() {
			gitLog = collectGitLog(ctx, openclawPath)
		}
	}()

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
	var runtimeSessions sessionSnapshot
	var tasks []map[string]any
	var sessionErr, taskErr error
	var runtimeHealth, runtimeInventories map[string]any
	var modelReadiness []map[string]any
	var modelsErr error
	var healthStatuses, inventoryStatuses map[string]CollectionStatus
	usageRanges := []struct {
		suffix, period, start string
		result                runtimeUsage
		err                   error
	}{{suffix: "All", period: "all"}, {suffix: "Today", start: todayStr}, {suffix: "7d", start: date7d}, {suffix: "30d", start: date30d}}
	if modern {
		agentIDs := []string{}
		for _, a := range agentConfig["agents"].([]any) {
			if id := jsonStr(asObj(a), "id"); id != "" {
				agentIDs = append(agentIDs, id)
			}
		}
		if len(agentIDs) == 0 {
			agentIDs = []string{"main"}
		}
		cwg.Add(3)
		go func() { defer cwg.Done(); modelReadiness, modelsErr = collectRuntimeModels(ctx, client, agentIDs) }()
		go func() { defer cwg.Done(); runtimeHealth, healthStatuses = collectRuntimeHealth(ctx, client, agentIDs) }()
		go func() {
			defer cwg.Done()
			runtimeInventories, inventoryStatuses = collectRuntimeInventories(ctx, client)
		}()
		cwg.Add(2)
		go func() {
			defer cwg.Done()
			runtimeSessions, sessionErr = collectRuntimeSessions(ctx, client, loc, modelAliases)
		}()
		go func() { defer cwg.Done(); tasks, taskErr = collectRuntimeTasks(ctx, client, loc) }()
		for i := range usageRanges {
			cwg.Go(func() {
				r := &usageRanges[i]
				r.result, r.err = collectRuntimeUsage(ctx, client, r.period, r.start, todayStr, loc.String())
			})
		}
	}

	// Build group names from session data for bindings
	groupNames := buildGroupNames(sessionStores)
	enrichBindings(agentConfig, groupNames)

	// Wait for gateway before building sessions (sessions need gateway map)
	cwg.Wait()
	collections["crons"] = collectionStatus("cli.cron.list", cronErr, cronErr == nil)
	if modern {
		collections["crons"] = collectionStatus("gateway.cron.list", cronErr, cronErr == nil)
	}
	if cronErr != nil && !modern && crons != nil {
		// The file rows are real, but the authoritative CLI never answered, so
		// the snapshot may be arbitrarily out of date. Report the fallback and
		// the failure rather than a healthy collection.
		collections["crons"] = partialCollectionStatus("legacy.cron.files", appopenclaw.ErrorCode(cronErr))
	}

	// Sessions
	knownSIDs := map[string]string{}
	sessionLiveModelTTL := time.Duration(cfg.Refresh.IntervalSeconds) * time.Second
	var sessionsList []map[string]any
	if modern {
		sessionsList = runtimeSessions.Rows
		collections["sessions"] = collectionStatus("gateway.sessions.list", sessionErr, runtimeSessions.Complete)
		for _, row := range sessionsList {
			if sid := jsonStr(row, "sessionId"); sid != "" {
				knownSIDs[sid] = jsonStr(row, "type")
			}
		}
		collections["tasks"] = collectionStatus("gateway.tasks.list", taskErr, taskErr == nil)
		if errors.Is(taskErr, errTaskRowLimit) {
			// Truncation is not an outage: keep the collected rows visible and
			// say the page walk stopped short, matching the sessions collector.
			collections["tasks"] = partialCollectionStatus("gateway.tasks.list", "row_limit")
		}
	} else {
		sessionsList = collectSessions(ctx, sessionStores, basePath, loc, now, modelAliases, knownSIDs, sessionLiveModelTTL)
		collections["sessions"] = collectionStatus("legacy.session.files", nil, true)
	}

	if !modern {
		if cliChannelStatus, ok := channelStatusCollector(ctx, nil, nil); ok {
			overlayChannelStatus(agentConfig, cliChannelStatus)
		}
	}

	// Backfill channel connectivity: gateway /readyz failing[] is authoritative
	// for failures; on probe failure we fall back to the session-activity
	// heuristic (failing is nil, so no channel is blanked).
	if !modern && !appopenclaw.TargetFromContext(ctx).IsContainer() {
		readyzFailing, _ := readyzProbe(ctx, cfg.AI.GatewayPort)
		backfillChannelConnectivity(agentConfig, sessionsList, readyzFailing)
	} else {
		agentConfig["channelStatus"] = map[string]any{}
	}

	// Token usage from JSONL
	modelsAll := map[string]*TokenBucket{}
	modelsToday := map[string]*TokenBucket{}
	models7d := map[string]*TokenBucket{}
	models30d := map[string]*TokenBucket{}
	subagentAll := map[string]*TokenBucket{}
	subagentToday := map[string]*TokenBucket{}
	subagent7d := map[string]*TokenBucket{}
	subagent30d := map[string]*TokenBucket{}

	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	// Build sessionId → session key map
	sidToKey := BuildSIDToKeyMap(sessionStores)

	// CollectTokenUsageWithCache drives the main per-model token usage and cost
	// buckets (the primary Token Usage panel). Its subagent-run output is now
	// empty — OpenClaw 2026.6+ moved subagent tracking out of session keys — so
	// its return is discarded; subagent runs come from the durable tasks store.
	if !modern {
		CollectTokenUsageWithCache(
			filepath.Join(dashboardDir, ".token-usage-cache.json"),
			basePath, loc, todayStr, date7d, date30d,
			knownSIDs, sidToKey, modelAliases,
			modelsAll, modelsToday, models7d, models30d,
			subagentAll, subagentToday, subagent7d, subagent30d,
			dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
		)
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
		subagentRuns = collectSubagentRuns(ctx, nil, nil, loc)
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
	dailyChart := BuildDailyChart(now, dailyCosts, dailyTokens, dailyCalls,
		dailySubagentCosts, dailySubagentCount, dashboardDir)

	// Alerts
	totalCostToday := sumBucketCosts(modelsToday)
	totalCostAll := sumBucketCosts(modelsAll)

	alertCostToday := totalCostToday
	if modern {
		// Modern collectors do not populate legacy token buckets. Alert only on
		// a complete current total, never an incomplete pricing subtotal.
		for _, r := range usageRanges {
			if r.suffix == "Today" && r.err == nil && r.result.CompleteCost() {
				alertCostToday = *r.result.Totals.TotalCost
			}
		}
	}
	alerts := BuildAlerts(alertCostToday, costThresholdHigh, costThresholdWarn,
		crons, sessionsList, contextThreshold, gateway, memoryThresholdKB)

	// Cost breakdown
	costBreakdown := BuildCostBreakdown(modelsAll)
	costBreakdownToday := BuildCostBreakdown(modelsToday)

	// Projected monthly
	projectedMonthly := totalCostToday * 30

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
		"lastRefresh":   now.Format("2006-01-02 15:04:05 ") + tzName,
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
		"subagentCostAllTime": round2(sumBucketCosts(subagentAll)),
		"subagentCostToday":   round2(sumBucketCosts(subagentToday)),
		"subagentCost7d":      round2(sumBucketCosts(subagent7d)),
		"subagentCost30d":     round2(sumBucketCosts(subagent30d)),

		"tokenUsage":         BucketsToList(modelsAll),
		"tokenUsageToday":    BucketsToList(modelsToday),
		"tokenUsage7d":       BucketsToList(models7d),
		"tokenUsage30d":      BucketsToList(models30d),
		"subagentUsage":      BucketsToList(subagentAll),
		"subagentUsageToday": BucketsToList(subagentToday),
		"subagentUsage7d":    BucketsToList(subagent7d),
		"subagentUsage30d":   BucketsToList(subagent30d),

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
		data["sessions"] = sessionsList
		data["sessionCount"] = runtimeSessions.Total
		data["sessionTotal"] = runtimeSessions.Total
		data["modelReadiness"] = modelReadiness
		collections["modelReadiness"] = collectionStatus("cli.models.status", modelsErr, modelsErr == nil)
		maps.Copy(data, runtimeHealth)
		maps.Copy(data, runtimeInventories)
		maps.Copy(collections, healthStatuses)
		maps.Copy(collections, inventoryStatuses)
		if configErr != nil {
			// Without configuration the agent roster is the guessed ["main"],
			// so these per-agent collections cover an unknown fraction of the
			// runtime. Keep the rows, drop the claim that they are complete.
			for _, name := range []string{"memory", "skillInventory", "modelReadiness"} {
				if status, ok := collections[name]; ok && status.State == "ready" {
					collections[name] = partialCollectionStatus(status.Source, "configuration_unavailable")
				}
			}
		}
		for _, r := range usageRanges {
			collections["usage"+r.suffix] = collectionStatus("gateway.sessions.usage", r.err, r.result.CompleteTokens())
			applyRuntimeUsage(data, r.suffix, r.result, now)
		}
		for _, field := range []string{"subagentCostAllTime", "subagentCostToday", "subagentCost7d", "subagentCost30d"} {
			data[field] = nil
		}
	}
	return data
}
