// Package apprefresh collects dashboard data including sessions, tokens, crons, and gateway health.
package apprefresh

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// RunRefreshCollector generates data.json from OpenClaw's filesystem data.
func RunRefreshCollector(ctx context.Context, dashboardDir, openclawPath string, cfgOpt ...appconfig.Config) error {
	var cfg appconfig.Config
	if len(cfgOpt) > 0 {
		cfg = cfgOpt[0]
	} else {
		cfg = appconfig.Load(dashboardDir)
	}
	data := collectDashboardData(ctx, dashboardDir, openclawPath, cfg)

	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data.json: %w", err)
	}

	tmpPath := filepath.Join(dashboardDir, "data.json.tmp")
	finalPath := filepath.Join(dashboardDir, "data.json")

	if err := os.WriteFile(tmpPath, out, 0644); err != nil {
		return fmt.Errorf("write data.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename data.json.tmp: %w", err)
	}
	return nil
}

var reStripTelegramID = regexp.MustCompile(`\s*id[:\-]\s*-?\d+`)

// titleCase uppercases the first byte of an ASCII string.
// Replaces deprecated strings.Title for the simple provider-name case.
func TitleCase(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

func TrimLabel(s string) string {
	if s == "" {
		return s
	}
	return strings.TrimSpace(reStripTelegramID.ReplaceAllString(s, ""))
}

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

func collectGatewayHealth(ctx context.Context) map[string]any {
	gw := map[string]any{
		"status": "offline",
		"pid":    nil,
		"uptime": "",
		"memory": "",
		"rss":    0,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pgrepCtx, pgrepCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pgrepCancel()

	out, err := exec.CommandContext(pgrepCtx, "pgrep", "-f", "openclaw-gateway").Output()
	if err != nil {
		return gw
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	myPid := strconv.Itoa(os.Getpid())
	var pid string
	for _, p := range pids {
		if p != "" && p != myPid {
			pid = p
			break
		}
	}
	if pid == "" {
		return gw
	}

	pidInt, err := strconv.Atoi(pid)
	if err != nil {
		return gw
	}
	gw["pid"] = pidInt
	gw["status"] = "online"

	psCtx, psCancel := context.WithTimeout(ctx, 5*time.Second)
	defer psCancel()
	psOut, err := exec.CommandContext(psCtx, "ps", "-p", pid, "-o", "etime=,rss=").Output()
	if err != nil {
		return gw
	}
	parts := strings.Fields(strings.TrimSpace(string(psOut)))
	if len(parts) >= 2 {
		gw["uptime"] = strings.TrimSpace(parts[0])
		rssKB, _ := strconv.Atoi(parts[1])
		gw["rss"] = rssKB
		switch {
		case rssKB > 1048576:
			gw["memory"] = fmt.Sprintf("%.1f GB", float64(rssKB)/1048576)
		case rssKB > 1024:
			gw["memory"] = fmt.Sprintf("%.0f MB", float64(rssKB)/1024)
		default:
			gw["memory"] = fmt.Sprintf("%d KB", rssKB)
		}
	}
	return gw
}

func defaultAgentConfig() map[string]any {
	return map[string]any{
		"primaryModel": "", "primaryModelId": "",
		"imageModel": "", "imageModelId": "",
		"fallbacks": []string{}, "streamMode": "off",
		"telegramDmPolicy": "—", "telegramGroups": 0,
		"channels": []string{}, "channelStatus": map[string]any{},
		"compaction": map[string]any{}, "agents": []any{},
		"search": map[string]any{}, "gateway": map[string]any{},
		"hooks": []any{}, "plugins": []string{},
		"skills": []any{}, "bindings": []any{},
		"crons": []any{}, "tts": false, "diagnostics": false,
	}
}

func parseOpenclawConfig(oc map[string]any, basePath string) (
	compactionMode string,
	skills []map[string]any,
	availableModels []map[string]any,
	modelAliases map[string]string,
	agentConfig map[string]any,
) {
	modelAliases = map[string]string{}

	agents := jsonObj(oc, "agents")
	defaults := jsonObj(agents, "defaults")

	// Compaction
	compaction := jsonObj(defaults, "compaction")
	if m, ok := compaction["mode"].(string); ok {
		compactionMode = m
	} else {
		compactionMode = "auto"
	}

	// Skills
	skillEntries := jsonObj(jsonObj(oc, "skills"), "entries")
	for _, name := range sortedJSONKeys(skillEntries) {
		conf := skillEntries[name]
		enabled := true
		if cm, ok := conf.(map[string]any); ok {
			if e, ok := cm["enabled"].(bool); ok {
				enabled = e
			}
		}
		skills = append(skills, map[string]any{"name": name, "active": enabled, "type": "builtin"})
	}

	// Models
	modelCfg := jsonObj(defaults, "model")
	primary := jsonStr(modelCfg, "primary")
	fallbacksRaw := jsonArr(modelCfg, "fallbacks")
	var fallbacks []string
	for _, f := range fallbacksRaw {
		if s, ok := f.(string); ok {
			fallbacks = append(fallbacks, s)
		}
	}
	imageModel := jsonStr(jsonObj(defaults, "imageModel"), "primary")

	models := jsonObj(defaults, "models")
	for _, mid := range sortedJSONKeys(models) {
		mconf := models[mid]
		mc := asObj(mconf)
		alias := jsonStr(mc, "alias")
		if alias == "" {
			alias = mid
		}
		modelAliases[mid] = alias
		provider := "unknown"
		if strings.Contains(mid, "/") {
			provider = TitleCase(strings.SplitN(mid, "/", 2)[0])
		}
		status := "available"
		if mid == primary {
			status = "active"
		}
		availableModels = append(availableModels, map[string]any{
			"provider": provider, "name": alias, "id": mid, "status": status,
		})
	}

	// Channels
	channelsCfg := jsonObj(oc, "channels")
	tgCfg := jsonObj(channelsCfg, "telegram")
	var channelsEnabled []string
	channelStatus := map[string]any{}
	for _, chName := range sortedJSONKeys(channelsCfg) {
		conf := channelsCfg[chName]
		cm := asObj(conf)
		if cm == nil {
			continue
		}
		enabled := true
		if e, ok := cm["enabled"].(bool); ok {
			enabled = e
		}
		if enabled {
			channelsEnabled = append(channelsEnabled, chName)
		}

		configured := false
		if c, ok := cm["configured"].(bool); ok {
			configured = c
		} else {
			for k := range cm {
				if k != "enabled" && k != "configured" && k != "connected" && k != "health" && k != "error" && k != "lastError" {
					configured = true
					break
				}
			}
		}

		var connected any
		if c, ok := cm["connected"]; ok {
			connected = c
		}
		health := cm["health"]
		errorMsg := cm["error"]
		if errorMsg == nil {
			errorMsg = cm["lastError"]
		}

		if hm, ok := health.(map[string]any); ok {
			if c, ok := hm["connected"]; ok && connected == nil {
				connected = c
			}
			if e := hm["error"]; e != nil {
				errorMsg = e
			} else if e := hm["lastError"]; e != nil {
				errorMsg = e
			}
		} else if hs, ok := health.(string); ok && connected == nil {
			switch strings.ToLower(hs) {
			case "connected", "ok", "healthy", "online":
				connected = true
			case "disconnected", "offline", "error", "unhealthy":
				connected = false
			}
		}

		channelStatus[chName] = map[string]any{
			"enabled":    enabled,
			"configured": configured,
			"connected":  connected,
			"health":     health,
			"error":      errorMsg,
		}
	}

	// Search / web tools
	webCfg := jsonObj(jsonObj(jsonObj(oc, "tools"), "web"), "search")

	// Gateway config
	gwCfg := jsonObj(oc, "gateway")

	// Hooks
	hookEntries := jsonObj(jsonObj(jsonObj(oc, "hooks"), "internal"), "entries")
	var hooksList []any
	for _, n := range sortedJSONKeys(hookEntries) {
		v := hookEntries[n]
		hm := asObj(v)
		enabled := true
		if hm != nil {
			if e, ok := hm["enabled"].(bool); ok {
				enabled = e
			}
		}
		hooksList = append(hooksList, map[string]any{"name": n, "enabled": enabled})
	}

	// Plugins
	pluginEntries := jsonObj(jsonObj(oc, "plugins"), "entries")
	pluginsList := append([]string(nil), sortedJSONKeys(pluginEntries)...)

	// Skills config
	skillEntriesCfg := jsonObj(jsonObj(oc, "skills"), "entries")
	var skillsCfg []any
	for _, n := range sortedJSONKeys(skillEntriesCfg) {
		v := skillEntriesCfg[n]
		sm := asObj(v)
		enabled := true
		if sm != nil {
			if e, ok := sm["enabled"].(bool); ok {
				enabled = e
			}
		}
		skillsCfg = append(skillsCfg, map[string]any{"name": n, "enabled": enabled})
	}

	// Bindings
	bindings := jsonArr(oc, "bindings")
	var bindingsList []any
	for _, b := range bindings {
		bm := asObj(b)
		if bm == nil {
			continue
		}
		match := asObj(bm["match"])
		peer := asObj(match["peer"])
		bindingsList = append(bindingsList, map[string]any{
			"agentId": jsonStr(bm, "agentId"),
			"channel": jsonStr(match, "channel"),
			"kind":    jsonStr(peer, "kind"),
			"id":      jsonStr(peer, "id"),
			"name":    "",
		})
	}

	// Default agent entry for unmatched channels
	agentList := jsonArr(agents, "list")
	defaultAgent := "main"
	for _, a := range agentList {
		am := asObj(a)
		if am != nil {
			if d, ok := am["default"].(bool); ok && d {
				if id, ok := am["id"].(string); ok {
					defaultAgent = id
				}
			}
		}
	}
	bindingsList = append(bindingsList, map[string]any{
		"agentId": defaultAgent, "channel": "all", "kind": "default",
		"id": "", "name": "All unmatched channels",
	})

	// TTS
	hasTTS := jsonStr(jsonObj(oc, "talk"), "apiKey") != ""

	// Diagnostics
	diagEnabled := false
	if d, ok := jsonObj(oc, "diagnostics")["enabled"].(bool); ok {
		diagEnabled = d
	}

	// Model params
	modelParams := map[string]map[string]any{}
	for _, mid := range sortedJSONKeys(models) {
		mconf := models[mid]
		mc := asObj(mconf)
		if mc != nil {
			if p, ok := mc["params"].(map[string]any); ok {
				modelParams[mid] = p
			}
		}
	}

	// Build available models list
	var availModels []any
	for _, mid := range sortedJSONKeys(models) {
		mconf := models[mid]
		mc := asObj(mconf)
		alias := jsonStr(mc, "alias")
		if alias == "" {
			alias = mid
		}
		prov := "—"
		if strings.Contains(mid, "/") {
			prov = strings.SplitN(mid, "/", 2)[0]
		}
		availModels = append(availModels, map[string]any{
			"id": mid, "alias": alias, "provider": prov,
		})
	}

	// Build agent entries
	var agentEntries []any
	if len(agentList) > 0 {
		for i, ag := range agentList {
			am := asObj(ag)
			if am == nil {
				continue
			}
			aid := jsonStr(am, "id")
			if aid == "" {
				aid = fmt.Sprintf("agent-%d", i)
			}
			amodel := primary
			var agentFallbacks []string
			if mc, ok := am["model"]; ok {
				if mcm, ok := mc.(map[string]any); ok {
					if p := jsonStr(mcm, "primary"); p != "" {
						amodel = p
					}
					for _, f := range jsonArr(mcm, "fallbacks") {
						if s, ok := f.(string); ok {
							agentFallbacks = append(agentFallbacks, s)
						}
					}
				} else if ms, ok := mc.(string); ok {
					amodel = ms
				}
			}
			if len(agentFallbacks) == 0 {
				agentFallbacks = fallbacks
			}
			isDefault := false
			if d, ok := am["default"].(bool); ok {
				isDefault = d
			}
			role := jsonStr(am, "role")
			if role == "" {
				if isDefault {
					role = "Default"
				} else {
					role = TitleCase(strings.ReplaceAll(aid, "-", " "))
				}
			}
			params := modelParams[amodel]
			var ctx1m any
			if params != nil {
				ctx1m = params["context1m"]
			}
			var fb []string
			for i, f := range agentFallbacks {
				if i >= 3 {
					break
				}
				if a, ok := modelAliases[f]; ok {
					fb = append(fb, a)
				} else {
					fb = append(fb, f)
				}
			}
			entry := map[string]any{
				"id": aid, "role": role,
				"model":     aliasOrID(modelAliases, amodel),
				"modelId":   amodel,
				"workspace": jsonStrDefault(am, "workspace", "~/.openclaw/workspace"),
				"isDefault": isDefault,
				"context1m": ctx1m,
				"fallbacks": fb,
			}
			agentEntries = append(agentEntries, entry)
		}
	} else {
		params := modelParams[primary]
		var ctx1m any
		if params != nil {
			ctx1m = params["context1m"]
		}
		agentEntries = append(agentEntries, map[string]any{
			"id": "default", "role": "Default",
			"model": aliasOrID(modelAliases, primary), "modelId": primary,
			"workspace": "~/.openclaw/workspace", "isDefault": true,
			"context1m": ctx1m,
		})
	}

	var fb3 []string
	for i, f := range fallbacks {
		if i >= 3 {
			break
		}
		fb3 = append(fb3, aliasOrID(modelAliases, f))
	}

	agentConfig = map[string]any{
		"primaryModel":     aliasOrID(modelAliases, primary),
		"primaryModelId":   primary,
		"imageModel":       aliasOrID(modelAliases, imageModel),
		"imageModelId":     imageModel,
		"fallbacks":        fb3,
		"streamMode":       jsonStrDefault(tgCfg, "streamMode", "off"),
		"telegramDmPolicy": jsonStrDefault(tgCfg, "dmPolicy", "—"),
		"telegramGroups":   len(asObj(tgCfg["groups"])),
		"channels":         channelsEnabled,
		"channelStatus":    channelStatus,
		"compaction": map[string]any{
			"mode":               jsonStrDefault(compaction, "mode", "auto"),
			"reserveTokensFloor": compaction["reserveTokensFloor"],
			"memoryFlush":        compaction["memoryFlush"],
			"softThresholdTokens": func() any {
				mf := asObj(compaction["memoryFlush"])
				if mf != nil {
					return mf["softThresholdTokens"]
				}
				return 0
			}(),
		},
		"search": map[string]any{
			"provider":        jsonStrDefault(webCfg, "provider", "—"),
			"maxResults":      webCfg["maxResults"],
			"cacheTtlMinutes": webCfg["cacheTtlMinutes"],
		},
		"gateway": map[string]any{
			"port":     gwCfg["port"],
			"mode":     gwCfg["mode"],
			"bind":     gwCfg["bind"],
			"authMode": jsonStr(asObj(gwCfg["auth"]), "mode"),
			"tailscale": func() any {
				ts := asObj(gwCfg["tailscale"])
				if ts != nil {
					return jsonStrDefault(ts, "mode", "off")
				}
				return "off"
			}(),
		},
		"hooks":           hooksList,
		"plugins":         pluginsList,
		"skills":          skillsCfg,
		"bindings":        bindingsList,
		"tts":             hasTTS,
		"diagnostics":     diagEnabled,
		"agents":          agentEntries,
		"availableModels": availModels,
		"subagentConfig": map[string]any{
			"maxConcurrent":       jsonObj(defaults, "subagents")["maxConcurrent"],
			"maxSpawnDepth":       jsonObj(defaults, "subagents")["maxSpawnDepth"],
			"maxChildrenPerAgent": jsonObj(defaults, "subagents")["maxChildrenPerAgent"],
		},
	}

	return compactionMode, skills, availableModels, modelAliases, agentConfig
}

func CollectCrons(cronPath string, loc *time.Location) []map[string]any {
	var crons []map[string]any
	data, err := os.ReadFile(cronPath)
	if err != nil {
		return crons
	}
	var cronFile map[string]any
	if err := json.Unmarshal(data, &cronFile); err != nil {
		return crons
	}
	jobs, ok := cronFile["jobs"].([]any)
	if !ok {
		return crons
	}

	for _, job := range jobs {
		jm := asObj(job)
		if jm == nil {
			continue
		}
		sched := asObj(jm["schedule"])
		kind := jsonStr(sched, "kind")
		var schedStr string
		switch kind {
		case "cron":
			schedStr = jsonStr(sched, "expr")
		case "every":
			ms, _ := sched["everyMs"].(float64)
			msInt := int64(ms)
			switch {
			case msInt >= 86400000:
				schedStr = fmt.Sprintf("Every %dd", msInt/86400000)
			case msInt >= 3600000:
				schedStr = fmt.Sprintf("Every %dh", msInt/3600000)
			case msInt >= 60000:
				schedStr = fmt.Sprintf("Every %dm", msInt/60000)
			default:
				schedStr = fmt.Sprintf("Every %dms", msInt)
			}
		case "at":
			at := jsonStr(sched, "at")
			if len(at) > 16 {
				at = at[:16]
			}
			schedStr = at
		default:
			b, _ := json.Marshal(sched)
			schedStr = string(b)
		}

		state := asObj(jm["state"])
		lastStatus := jsonStrDefault(state, "lastStatus", "none")
		lastRunMs, _ := state["lastRunAtMs"].(float64)
		nextRunMs, _ := state["nextRunAtMs"].(float64)
		durationMs, _ := state["lastDurationMs"].(float64)

		var lastRunStr, nextRunStr string
		if lastRunMs > 0 {
			lastRunStr = time.UnixMilli(int64(lastRunMs)).In(loc).Format("2006-01-02 15:04")
		}
		if nextRunMs > 0 {
			nextRunStr = time.UnixMilli(int64(nextRunMs)).In(loc).Format("2006-01-02 15:04")
		}

		enabled := true
		if e, ok := jm["enabled"].(bool); ok {
			enabled = e
		}

		payload := asObj(jm["payload"])
		model := jsonStr(payload, "model")

		name := jsonStrDefault(jm, "name", "Unknown")

		crons = append(crons, map[string]any{
			"name":           name,
			"schedule":       schedStr,
			"enabled":        enabled,
			"lastRun":        lastRunStr,
			"lastStatus":     lastStatus,
			"lastDurationMs": int(durationMs),
			"nextRun":        nextRunStr,
			"model":          model,
		})
	}
	return crons
}

func BuildDailyChart(now time.Time, dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int, dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64, dailySubagentCount map[string]int,
	dashboardDir string) []map[string]any {

	// Generate last 30 days
	var chartDates []string
	for i := 29; i >= 0; i-- {
		chartDates = append(chartDates, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}

	// Find top 6 models by cost
	modelTotals := map[string]float64{}
	for _, d := range chartDates {
		for m, c := range dailyCosts[d] {
			modelTotals[m] += c
		}
	}
	type modelCost struct {
		model string
		cost  float64
	}
	var sorted []modelCost
	for m, c := range modelTotals {
		sorted = append(sorted, modelCost{m, c})
	}
	slices.SortFunc(sorted, func(a, b modelCost) int { return cmp.Compare(b.cost, a.cost) })
	topModels := map[string]bool{}
	for i, mc := range sorted {
		if i >= 6 {
			break
		}
		topModels[mc.model] = true
	}

	var chart []map[string]any
	for _, d := range chartDates {
		dayModels := dailyCosts[d]
		dayTokens := dailyTokens[d]
		dayCalls := dailyCalls[d]

		var totalCost float64
		for _, c := range dayModels {
			totalCost += c
		}
		var totalTokens int
		for _, t := range dayTokens {
			totalTokens += t
		}
		var totalCalls int
		for _, c := range dayCalls {
			totalCalls += c
		}

		models := map[string]any{}
		var otherCost float64
		for m, c := range dayModels {
			if topModels[m] {
				models[m] = math.Round(c*10000) / 10000
			} else {
				otherCost += c
			}
		}
		if otherCost > 0 {
			models["Other"] = math.Round(otherCost*10000) / 10000
		}

		chart = append(chart, map[string]any{
			"date":         d,
			"label":        d[5:],
			"total":        math.Round(totalCost*100) / 100,
			"tokens":       totalTokens,
			"calls":        totalCalls,
			"subagentCost": math.Round(dailySubagentCosts[d]*100) / 100,
			"subagentRuns": dailySubagentCount[d],
			"models":       models,
		})
	}

	// Merge frozen historical data
	frozenPath := filepath.Join(dashboardDir, "frozen-daily.json")
	if data, err := os.ReadFile(frozenPath); err == nil {
		var frozen map[string]map[string]any
		if json.Unmarshal(data, &frozen) == nil {
			for i, entry := range chart {
				d, _ := entry["date"].(string)
				if f, ok := frozen[d]; ok {
					fTotal, _ := f["total"].(float64)
					eTotal, _ := entry["total"].(float64)
					if fTotal > eTotal {
						chart[i]["total"] = math.Round(fTotal*100) / 100
						if t, ok := f["tokens"].(float64); ok {
							chart[i]["tokens"] = int(t)
						}
						if sr, ok := f["subagentRuns"].(float64); ok {
							chart[i]["subagentRuns"] = int(sr)
						}
						if sc, ok := f["subagentCost"].(float64); ok {
							chart[i]["subagentCost"] = math.Round(sc*100) / 100
						}
						if m, ok := f["models"].(map[string]any); ok {
							rounded := map[string]any{}
							for k, v := range m {
								if fv, ok := v.(float64); ok {
									rounded[k] = math.Round(fv*10000) / 10000
								}
							}
							chart[i]["models"] = rounded
						}
					}
				}
			}
		}
	}

	return chart
}

func collectGitLog(ctx context.Context, openclawPath string) []map[string]any {
	var gitLog []map[string]any
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", openclawPath, "log",
		"--oneline", "-5", "--format=%h|%s|%ar").Output()
	if err != nil {
		return gitLog
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if !strings.Contains(line, "|") {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		entry := map[string]any{"hash": parts[0], "message": parts[1]}
		if len(parts) > 2 {
			entry["ago"] = parts[2]
		} else {
			entry["ago"] = ""
		}
		gitLog = append(gitLog, entry)
	}
	return gitLog
}

func BuildAlerts(totalCostToday, costHigh, costWarn float64,
	crons []map[string]any, sessions []map[string]any,
	contextThreshold float64, gateway map[string]any, memThresholdKB float64) []map[string]any {

	var alerts []map[string]any

	if totalCostToday > costHigh {
		alerts = append(alerts, map[string]any{
			"type": "warning", "icon": "💰",
			"message":  fmt.Sprintf("High daily cost: $%.2f", totalCostToday),
			"severity": "high",
		})
	} else if totalCostToday > costWarn {
		alerts = append(alerts, map[string]any{
			"type": "info", "icon": "💵",
			"message":  fmt.Sprintf("Daily cost above $%.0f: $%.2f", costWarn, totalCostToday),
			"severity": "medium",
		})
	}

	for _, c := range crons {
		if c["lastStatus"] == "error" {
			name, _ := c["name"].(string)
			alerts = append(alerts, map[string]any{
				"type": "error", "icon": "❌",
				"message":  "Cron failed: " + name,
				"severity": "high",
			})
		}
	}

	for _, s := range sessions {
		pct, _ := s["contextPct"].(float64)
		if pct > contextThreshold {
			name, _ := s["name"].(string)
			if len(name) > 30 {
				name = name[:30]
			}
			alerts = append(alerts, map[string]any{
				"type": "warning", "icon": "⚠️",
				"message":  fmt.Sprintf("High context: %s (%.0f%%)", name, pct),
				"severity": "medium",
			})
		}
	}

	if gateway["status"] == "offline" {
		alerts = append(alerts, map[string]any{
			"type": "error", "icon": "🔴",
			"message": "Gateway is offline", "severity": "critical",
		})
	}

	rss, _ := gateway["rss"].(int)
	if rss > int(memThresholdKB) {
		mem, _ := gateway["memory"].(string)
		alerts = append(alerts, map[string]any{
			"type": "warning", "icon": "🧠",
			"message":  "High memory usage: " + mem,
			"severity": "medium",
		})
	}

	return alerts
}

func BuildCostBreakdown(m map[string]*TokenBucket) []map[string]any {
	type kv struct {
		model string
		cost  float64
	}
	var pairs []kv
	for k, v := range m {
		if v.Cost > 0 {
			pairs = append(pairs, kv{k, v.Cost})
		}
	}
	slices.SortFunc(pairs, func(a, b kv) int { return cmp.Compare(b.cost, a.cost) })
	var out []map[string]any
	for _, p := range pairs {
		out = append(out, map[string]any{
			"model": p.model,
			"cost":  math.Round(p.cost*100) / 100,
		})
	}
	return out
}

// JSON helper functions

func jsonObj(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func jsonArr(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

func jsonStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func jsonStrDefault(m map[string]any, key, def string) string {
	s := jsonStr(m, key)
	if s == "" {
		return def
	}
	return s
}

func asObj(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func aliasOrID(aliases map[string]string, id string) string {
	if a, ok := aliases[id]; ok {
		return a
	}
	return id
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func sumBucketCosts(m map[string]*TokenBucket) float64 {
	var total float64
	for _, b := range m {
		total += b.Cost
	}
	return total
}

func FilterByDate(runs []map[string]any, targetDate, op string) []map[string]any {
	var out []map[string]any
	for _, r := range runs {
		d, _ := r["date"].(string)
		switch op {
		case "==":
			if d == targetDate {
				out = append(out, r)
			}
		case ">=":
			if d >= targetDate {
				out = append(out, r)
			}
		}
	}
	return out
}

func LimitSlice[T any](s []T, max int) []T {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func ensureMapMap(m map[string]map[string]float64, key string) map[string]float64 {
	if _, ok := m[key]; !ok {
		m[key] = map[string]float64{}
	}
	return m[key]
}

func ensureMapMapInt(m map[string]map[string]int, key string) map[string]int {
	if _, ok := m[key]; !ok {
		m[key] = map[string]int{}
	}
	return m[key]
}

func sortedJSONKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
