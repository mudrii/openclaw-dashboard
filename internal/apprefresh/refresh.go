// Package apprefresh collects dashboard data including sessions, tokens, crons, and gateway health.
package apprefresh

import (
	"context"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// RunRefreshCollector generates data.json from OpenClaw's filesystem data.
// Callers must supply the active dashboard Config; use appconfig.Load(dir) at
// the call site if no Config is on hand.
func RunRefreshCollector(ctx context.Context, dashboardDir, openclawPath string, cfg appconfig.Config) error {
	data := collectDashboardData(ctx, dashboardDir, openclawPath, cfg)

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data.json: %w", err)
	}

	tmpPath := filepath.Join(dashboardDir, "data.json.tmp")
	finalPath := filepath.Join(dashboardDir, "data.json")

	if err := os.WriteFile(tmpPath, out, 0o600); err != nil {
		return fmt.Errorf("write data.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename data.json.tmp: %w", err)
	}
	return nil
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

// ModelName returns the human-friendly display name for a model identifier.
// Falls back to the raw id when no friendly name is known.
func ModelName(model string) string {
	ml := strings.ToLower(model)
	if strings.Contains(ml, "/") {
		parts := strings.SplitN(ml, "/", 2)
		ml = parts[1]
	}
	switch {
	case strings.Contains(ml, "opus-4-6"):
		return "Claude Opus 4.6"
	case strings.Contains(ml, "opus"):
		return "Claude Opus 4.5"
	case strings.Contains(ml, "sonnet"):
		return "Claude Sonnet"
	case strings.Contains(ml, "haiku"):
		return "Claude Haiku"
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
	case strings.Contains(ml, "gemini") || strings.Contains(ml, "flash"):
		return "Gemini Flash"
	case strings.Contains(ml, "minimax-m2.5"):
		return "MiniMax M2.5"
	case strings.Contains(ml, "minimax-m2") || strings.Contains(ml, "minimax"):
		return "MiniMax"
	case strings.Contains(ml, "glm-5"):
		return "GLM-5"
	case strings.Contains(ml, "glm-4"):
		return "GLM-4"
	case strings.Contains(ml, "k2p5") || strings.Contains(ml, "kimi"):
		return "Kimi K2.5"
	case strings.Contains(ml, "gpt-5.3-codex"):
		return "GPT-5.3 Codex"
	case strings.Contains(ml, "gpt-5"):
		return "GPT-5"
	case strings.Contains(ml, "gpt-4o"):
		return "GPT-4o"
	case strings.Contains(ml, "gpt-4"):
		return "GPT-4"
	case strings.Contains(ml, "o1"):
		return "O1"
	case strings.Contains(ml, "o3"):
		return "O3"
	default:
		return model
	}
}

// collectDashboardData orchestrates parallel data collectors and assembles the
// dashboard JSON payload. Most work is delegated to refresh_*.go siblings.
func collectDashboardData(ctx context.Context, dashboardDir, openclawPath string, cfg appconfig.Config) map[string]any {
	now := time.Now()
	date7d := now.AddDate(0, 0, -7).Format("2006-01-02")
	date30d := now.AddDate(0, 0, -30).Format("2006-01-02")
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

	basePath := filepath.Join(openclawPath, "agents")
	configPath := filepath.Join(openclawPath, "openclaw.json")
	cronPath := filepath.Join(openclawPath, "cron/jobs.json")

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

	// Kick off independent collectors concurrently.
	var gateway map[string]any
	var crons []map[string]any
	var gitLog []map[string]any
	var cwg sync.WaitGroup
	cwg.Add(3)
	go func() { defer cwg.Done(); gateway = collectGatewayHealth(ctx) }()
	go func() { defer cwg.Done(); crons = CollectCrons(cronPath, loc) }()
	go func() { defer cwg.Done(); gitLog = collectGitLog(ctx, openclawPath) }()

	// OpenClaw config (file I/O — runs while subprocesses are in flight)
	compactionMode := "unknown"
	var skills []map[string]any
	var availableModels []map[string]any
	modelAliases := map[string]string{}
	agentConfig := defaultAgentConfig()

	if data, err := os.ReadFile(configPath); err == nil {
		var oc map[string]any
		if err := json.Unmarshal(data, &oc); err == nil {
			compactionMode, skills, availableModels, modelAliases, agentConfig =
				parseOpenclawConfig(oc, basePath)
		}
	}

	sessionStores := loadSessionStores(basePath)

	// Build group names from session data for bindings
	groupNames := buildGroupNames(sessionStores)
	enrichBindings(agentConfig, groupNames)

	// Wait for gateway before building sessions (sessions need gateway map)
	cwg.Wait()

	// Sessions
	knownSIDs := map[string]string{}
	sessionLiveModelTTL := time.Duration(cfg.Refresh.IntervalSeconds) * time.Second
	sessionsList := collectSessions(ctx, sessionStores, basePath, loc, now, modelAliases, knownSIDs, sessionLiveModelTTL)

	// Backfill channel connectivity from recent session activity
	backfillChannelConnectivity(agentConfig, sessionsList)

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

	subagentRuns := CollectTokenUsageWithCache(
		filepath.Join(dashboardDir, ".token-usage-cache.json"),
		basePath, loc, todayStr, date7d, date30d,
		knownSIDs, sidToKey, modelAliases,
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

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

	alerts := BuildAlerts(totalCostToday, costThresholdHigh, costThresholdWarn,
		crons, sessionsList, contextThreshold, gateway, memoryThresholdKB)

	// Cost breakdown
	costBreakdown := BuildCostBreakdown(modelsAll)
	costBreakdownToday := BuildCostBreakdown(modelsToday)

	// Projected monthly
	projectedMonthly := totalCostToday * 30

	return map[string]any{
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
}
