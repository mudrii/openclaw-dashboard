package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var refreshCollectorFunc = runRefreshCollector

// runRefreshCollector generates data.json from OpenClaw's filesystem data.
// This generates data.json from OpenClaw's filesystem data.
func runRefreshCollector(dashboardDir, openclawPath string) error {
	cfg := loadConfig(dashboardDir)
	data := collectDashboardData(dashboardDir, openclawPath, cfg)

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
		os.Remove(tmpPath)
		return fmt.Errorf("rename data.json.tmp: %w", err)
	}
	return nil
}

type sessionStoreFile struct {
	agentName string
	store     map[string]map[string]any
}

type tokenBucket struct {
	Calls     int     `json:"calls"`
	Input     int     `json:"input"`
	Output    int     `json:"output"`
	CacheRead int     `json:"cacheRead"`
	Total     int     `json:"totalTokens"`
	Cost      float64 `json:"cost"`
}

func (b *tokenBucket) add(inp, out, cr, tt int, cost float64) {
	b.Calls++
	b.Input += inp
	b.Output += out
	b.CacheRead += cr
	b.Total += tt
	b.Cost += cost
}

type tokenUsageEntry struct {
	Model          string  `json:"model"`
	Calls          int     `json:"calls"`
	Input          string  `json:"input"`
	Output         string  `json:"output"`
	CacheRead      string  `json:"cacheRead"`
	TotalTokens    string  `json:"totalTokens"`
	Cost           float64 `json:"cost"`
	InputRaw       int     `json:"inputRaw"`
	OutputRaw      int     `json:"outputRaw"`
	CacheReadRaw   int     `json:"cacheReadRaw"`
	TotalTokensRaw int     `json:"totalTokensRaw"`
}

func bucketsToList(m map[string]*tokenBucket) []tokenUsageEntry {
	type kv struct {
		k string
		v *tokenBucket
	}
	var pairs []kv
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v.Cost > pairs[j].v.Cost })

	out := make([]tokenUsageEntry, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, tokenUsageEntry{
			Model:          p.k,
			Calls:          p.v.Calls,
			Input:          fmtTokens(p.v.Input),
			Output:         fmtTokens(p.v.Output),
			CacheRead:      fmtTokens(p.v.CacheRead),
			TotalTokens:    fmtTokens(p.v.Total),
			Cost:           math.Round(p.v.Cost*100) / 100,
			InputRaw:       p.v.Input,
			OutputRaw:      p.v.Output,
			CacheReadRaw:   p.v.CacheRead,
			TotalTokensRaw: p.v.Total,
		})
	}
	return out
}

func fmtTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return strconv.Itoa(n)
}

func getBucket(m map[string]*tokenBucket, key string) *tokenBucket {
	b, ok := m[key]
	if !ok {
		b = &tokenBucket{}
		m[key] = b
	}
	return b
}

var reStripTelegramID = regexp.MustCompile(`\s*id[:\-]\s*-?\d+`)

func trimLabel(s string) string {
	if s == "" {
		return s
	}
	return strings.TrimSpace(reStripTelegramID.ReplaceAllString(s, ""))
}

func modelName(model string) string {
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

func collectDashboardData(dashboardDir, openclawPath string, cfg Config) map[string]any {
	now := time.Now()
	todayStr := now.Format("2006-01-02")
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
	todayStr = now.Format("2006-01-02")

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

	// Gateway health
	gateway := collectGatewayHealth()

	// OpenClaw config
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

	// Sessions
	knownSIDs := map[string]string{}
	sessionsList := collectSessions(sessionStores, basePath, loc, now, todayStr, modelAliases, knownSIDs, gateway)

	// Backfill channel connectivity from recent session activity
	backfillChannelConnectivity(agentConfig, sessionsList)

	// Cron jobs
	crons := collectCrons(cronPath, loc)

	// Token usage from JSONL
	modelsAll := map[string]*tokenBucket{}
	modelsToday := map[string]*tokenBucket{}
	models7d := map[string]*tokenBucket{}
	models30d := map[string]*tokenBucket{}
	subagentAll := map[string]*tokenBucket{}
	subagentToday := map[string]*tokenBucket{}
	subagent7d := map[string]*tokenBucket{}
	subagent30d := map[string]*tokenBucket{}

	dailyCosts := map[string]map[string]float64{}
	dailyTokens := map[string]map[string]int{}
	dailyCalls := map[string]map[string]int{}
	dailySubagentCosts := map[string]float64{}
	dailySubagentCount := map[string]int{}

	// Build sessionId → session key map
	sidToKey := buildSIDToKeyMap(sessionStores)

	subagentRuns := collectTokenUsage(
		basePath, loc, todayStr, date7d, date30d,
		knownSIDs, sidToKey, modelAliases,
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)

	sort.Slice(subagentRuns, func(i, j int) bool {
		return subagentRuns[i]["timestamp"].(string) > subagentRuns[j]["timestamp"].(string)
	})

	subagentRunsToday := filterByDate(subagentRuns, todayStr, "==")
	subagentRuns7d := filterByDate(subagentRuns, date7d, ">=")
	subagentRuns30d := filterByDate(subagentRuns, date30d, ">=")

	// Count subagent runs per day
	for _, r := range subagentRuns {
		d, _ := r["date"].(string)
		if d != "" {
			dailySubagentCount[d]++
		}
	}

	// Build daily chart data (last 30 days)
	dailyChart := buildDailyChart(now, dailyCosts, dailyTokens, dailyCalls,
		dailySubagentCosts, dailySubagentCount, dashboardDir)

	// Git log
	gitLog := collectGitLog(openclawPath)

	// Alerts
	totalCostToday := sumBucketCosts(modelsToday)
	totalCostAll := sumBucketCosts(modelsAll)

	alerts := buildAlerts(totalCostToday, costThresholdHigh, costThresholdWarn,
		crons, sessionsList, contextThreshold, gateway, memoryThresholdKB)

	// Cost breakdown
	costBreakdown := buildCostBreakdown(modelsAll)
	costBreakdownToday := buildCostBreakdown(modelsToday)

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

		"sessions":     limitSlice(sessionsList, 20),
		"sessionCount": len(knownSIDs),

		"crons": crons,

		"subagentRuns":        limitSlice(subagentRuns, 30),
		"subagentRunsToday":   limitSlice(subagentRunsToday, 20),
		"subagentRuns7d":      limitSlice(subagentRuns7d, 50),
		"subagentRuns30d":     limitSlice(subagentRuns30d, 100),
		"subagentCostAllTime": round2(sumBucketCosts(subagentAll)),
		"subagentCostToday":   round2(sumBucketCosts(subagentToday)),
		"subagentCost7d":      round2(sumBucketCosts(subagent7d)),
		"subagentCost30d":     round2(sumBucketCosts(subagent30d)),

		"tokenUsage":         bucketsToList(modelsAll),
		"tokenUsageToday":    bucketsToList(modelsToday),
		"tokenUsage7d":       bucketsToList(models7d),
		"tokenUsage30d":      bucketsToList(models30d),
		"subagentUsage":      bucketsToList(subagentAll),
		"subagentUsageToday": bucketsToList(subagentToday),
		"subagentUsage7d":    bucketsToList(subagent7d),
		"subagentUsage30d":   bucketsToList(subagent30d),

		"dailyChart": dailyChart,

		"availableModels": availableModels,
		"agentConfig":     agentConfig,
		"skills":          skills,

		"gitLog": gitLog,
		"alerts": alerts,
	}
}

func collectGatewayHealth() map[string]any {
	gw := map[string]any{
		"status": "offline",
		"pid":    nil,
		"uptime": "",
		"memory": "",
		"rss":    0,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "pgrep", "-f", "openclaw-gateway").Output()
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

	pidInt, _ := strconv.Atoi(pid)
	gw["pid"] = pidInt
	gw["status"] = "online"

	psOut, err := exec.CommandContext(ctx, "ps", "-p", pid, "-o", "etime=,rss=").Output()
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
	compactionMode = "unknown"
	modelAliases = map[string]string{}
	agentConfig = defaultAgentConfig()

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
	for name, conf := range skillEntries {
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
	for mid, mconf := range models {
		mc := asObj(mconf)
		alias := jsonStr(mc, "alias")
		if alias == "" {
			alias = mid
		}
		modelAliases[mid] = alias
		provider := "unknown"
		if strings.Contains(mid, "/") {
			provider = strings.Title(strings.SplitN(mid, "/", 2)[0])
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
	for chName, conf := range channelsCfg {
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
	for n, v := range hookEntries {
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
	var pluginsList []string
	for name := range pluginEntries {
		pluginsList = append(pluginsList, name)
	}

	// Skills config
	skillEntriesCfg := jsonObj(jsonObj(oc, "skills"), "entries")
	var skillsCfg []any
	for n, v := range skillEntriesCfg {
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
	for mid, mconf := range models {
		mc := asObj(mconf)
		if mc != nil {
			if p, ok := mc["params"].(map[string]any); ok {
				modelParams[mid] = p
			}
		}
	}

	// Build available models list
	var availModels []any
	for mid, mconf := range models {
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
					role = strings.Title(strings.ReplaceAll(aid, "-", " "))
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

func loadSessionStores(basePath string) []sessionStoreFile {
	var stores []sessionStoreFile
	pattern := filepath.Join(basePath, "*/sessions/sessions.json")
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var store map[string]map[string]any
		if err := json.Unmarshal(data, &store); err != nil {
			continue
		}
		rel, _ := filepath.Rel(basePath, f)
		agentName := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		stores = append(stores, sessionStoreFile{agentName: agentName, store: store})
	}
	return stores
}

func buildGroupNames(stores []sessionStoreFile) map[string]string {
	groupNames := map[string]string{}
	for _, sf := range stores {
		for key, val := range sf.store {
			if !strings.Contains(key, "group:") || strings.Contains(key, "topic") ||
				strings.Contains(key, "run:") || strings.Contains(key, "subagent") {
				continue
			}
			parts := strings.Split(key, "group:")
			if len(parts) < 2 {
				continue
			}
			gid := strings.SplitN(parts[1], ":", 2)[0]
			name := ""
			if s, ok := val["subject"].(string); ok && s != "" {
				name = s
			} else if s, ok := val["displayName"].(string); ok && s != "" {
				name = s
			}
			if name != "" && !strings.HasPrefix(name, "telegram:") {
				groupNames[gid] = name
			}
		}
	}
	return groupNames
}

func enrichBindings(agentConfig map[string]any, groupNames map[string]string) {
	bindings, ok := agentConfig["bindings"].([]any)
	if !ok {
		return
	}
	for _, b := range bindings {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		id, _ := bm["id"].(string)
		if id != "" {
			if name, ok := groupNames[id]; ok {
				bm["name"] = name
			}
		}
	}
}

func loadAgentDefaultModels(basePath string) map[string]string {
	defaults := map[string]string{"main": "unknown", "work": "unknown", "group": "unknown"}
	cfgPath := filepath.Join(basePath, "..", "openclaw.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return defaults
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaults
	}
	agents := jsonObj(cfg, "agents")
	defs := jsonObj(agents, "defaults")
	primary := jsonStr(jsonObj(defs, "model"), "primary")
	if primary == "" {
		primary = "unknown"
	}
	for name, v := range agents {
		if name == "defaults" {
			continue
		}
		vm := asObj(v)
		if vm == nil {
			continue
		}
		model := jsonStr(jsonObj(vm, "model"), "primary")
		if model == "" {
			model = primary
		}
		defaults[name] = model
	}
	for _, a := range []string{"main", "work", "group"} {
		if _, ok := defaults[a]; !ok {
			defaults[a] = primary
		}
	}
	return defaults
}

func getSessionModel(basePath, agentName, sessionID string, agentDefaults map[string]string) string {
	if sessionID != "" {
		jsonlPath := filepath.Join(basePath, agentName, "sessions", sessionID+".jsonl")
		f, err := os.Open(jsonlPath)
		if err == nil {
			defer f.Close()
			scanner := newLimitedScanner(f, 10)
			for scanner.Scan() {
				var obj map[string]any
				if err := json.Unmarshal(scanner.Bytes(), &obj); err != nil {
					continue
				}
				if obj["type"] == "model_change" {
					provider, _ := obj["provider"].(string)
					modelID, _ := obj["modelId"].(string)
					if provider != "" && modelID != "" {
						return provider + "/" + modelID
					}
				}
			}
		}
	}
	if m, ok := agentDefaults[agentName]; ok {
		return m
	}
	return "unknown"
}

type limitedScanner struct {
	scanner *lineScanner
	max     int
	count   int
}

type lineScanner struct {
	data   []byte
	offset int
	line   []byte
}

func newLimitedScanner(f *os.File, maxLines int) *limitedScanner {
	data := make([]byte, 0, 16*1024)
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil || len(data) > 64*1024 {
			break
		}
	}
	return &limitedScanner{
		scanner: &lineScanner{data: data},
		max:     maxLines,
	}
}

func (ls *limitedScanner) Scan() bool {
	if ls.count >= ls.max {
		return false
	}
	ls.count++
	return ls.scanner.scan()
}

func (ls *limitedScanner) Bytes() []byte {
	return ls.scanner.line
}

func (s *lineScanner) scan() bool {
	if s.offset >= len(s.data) {
		return false
	}
	end := s.offset
	for end < len(s.data) && s.data[end] != '\n' {
		end++
	}
	s.line = s.data[s.offset:end]
	s.offset = end + 1
	return true
}

func collectSessions(stores []sessionStoreFile, basePath string, loc *time.Location, now time.Time, todayStr string,
	modelAliases map[string]string, knownSIDs map[string]string,
	gateway map[string]any) []map[string]any {

	agentDefaults := loadAgentDefaultModels(basePath)

	// Gateway API query for live session model info
	gatewayModelMap := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "openclaw", "sessions", "--json").Output()
	if err == nil && len(out) > 0 {
		var sessions any
		if json.Unmarshal(out, &sessions) == nil {
			switch s := sessions.(type) {
			case []any:
				for _, gs := range s {
					gm := asObj(gs)
					key, _ := gm["key"].(string)
					model, _ := gm["model"].(string)
					if key != "" && model != "" {
						gatewayModelMap[key] = model
					}
				}
			case map[string]any:
				if arr, ok := s["sessions"].([]any); ok {
					for _, gs := range arr {
						gm := asObj(gs)
						key, _ := gm["key"].(string)
						model, _ := gm["model"].(string)
						if key != "" && model != "" {
							gatewayModelMap[key] = model
						}
					}
				}
			}
		}
	}

	var sessionsList []map[string]any
	for _, sf := range stores {
		agentName := sf.agentName
		for key, val := range sf.store {
			sid, _ := val["sessionId"].(string)
			if sid == "" {
				continue
			}
			if strings.Contains(key, ":run:") {
				continue
			}

			var stype string
			switch {
			case strings.Contains(key, "cron:"):
				stype = "cron"
			case strings.Contains(key, "subagent:"):
				stype = "subagent"
			case strings.Contains(key, "group:"):
				stype = "group"
			case strings.Contains(key, "telegram"):
				stype = "telegram"
			case strings.HasSuffix(key, ":main"):
				stype = "main"
			default:
				stype = "other"
			}
			knownSIDs[sid] = stype

			ctxTokens, _ := val["contextTokens"].(float64)
			totalTokens, _ := val["totalTokens"].(float64)
			var ctxPct float64
			if ctxTokens > 0 {
				ctxPct = math.Round(totalTokens/ctxTokens*1000) / 10
			}

			updated, _ := val["updatedAt"].(float64)
			var updatedStr string
			var ageMin float64 = 9999
			if updated > 0 {
				updatedDt := time.UnixMilli(int64(updated)).In(loc)
				updatedStr = updatedDt.Format("15:04:05")
				ageMin = now.Sub(updatedDt).Minutes()
			}

			if ageMin >= 1440 {
				continue
			}

			rawLabel, _ := val["label"].(string)
			subject, _ := val["subject"].(string)
			var originLabel string
			if origin, ok := val["origin"].(map[string]any); ok {
				originLabel, _ = origin["label"].(string)
			}

			keyShort := key
			for _, pfx := range []string{"agent:work:", "agent:main:", "agent:group:"} {
				if strings.HasPrefix(key, pfx) {
					keyShort = key[len(pfx):]
					break
				}
			}

			displayName := trimLabel(rawLabel)
			if displayName == "" {
				displayName = trimLabel(subject)
			}
			if displayName == "" {
				displayName = trimLabel(originLabel)
			}
			if displayName == "" {
				displayName = keyShort
			}
			if len(displayName) > 50 {
				displayName = displayName[:50]
			}

			trigger := subject
			if trigger == "" {
				trigger = originLabel
			}
			if trigger == "" {
				trigger = rawLabel
			}
			if len(trigger) > 50 {
				trigger = trigger[:50]
			}

			// Resolve model with priority chain
			gwModel := gatewayModelMap[key]
			provOverride, _ := val["providerOverride"].(string)
			modelOverride, _ := val["modelOverride"].(string)

			var resolvedModel string
			if gwModel != "" {
				resolvedModel = gwModel
			} else if provOverride != "" && modelOverride != "" {
				resolvedModel = provOverride + "/" + modelOverride
			} else {
				m, _ := val["model"].(string)
				if m != "" {
					resolvedModel = m
				} else {
					resolvedModel = getSessionModel(basePath, agentName, sid, agentDefaults)
				}
			}
			if resolvedModel == "" || resolvedModel == "unknown" {
				resolvedModel = getSessionModel(basePath, agentName, sid, agentDefaults)
			}
			resolvedModel = aliasOrID(modelAliases, resolvedModel)

			if ctxPct > 100 {
				ctxPct = 100
			}

			spawnedBy, _ := val["spawnedBy"].(string)

			sessionsList = append(sessionsList, map[string]any{
				"name":         displayName,
				"key":          key,
				"agent":        agentName,
				"model":        resolvedModel,
				"contextPct":   ctxPct,
				"lastActivity": updatedStr,
				"updatedAt":    updated,
				"totalTokens":  int(totalTokens),
				"type":         stype,
				"spawnedBy":    spawnedBy,
				"active":       ageMin < 30,
				"label":        rawLabel,
				"subject":      trigger,
			})
		}
	}

	sort.Slice(sessionsList, func(i, j int) bool {
		ui, _ := sessionsList[i]["updatedAt"].(float64)
		uj, _ := sessionsList[j]["updatedAt"].(float64)
		return ui > uj
	})
	return sessionsList
}

func backfillChannelConnectivity(agentConfig map[string]any, sessions []map[string]any) {
	channelActive := map[string]bool{}
	for _, s := range sessions {
		key, _ := s["key"].(string)
		parts := strings.Split(key, ":")
		if len(parts) < 4 || parts[0] != "agent" {
			continue
		}
		ch := parts[2]
		if ch == "main" || ch == "cron" || ch == "subagent" || ch == "run" {
			continue
		}
		active, _ := s["active"].(bool)
		if active {
			channelActive[ch] = true
		}
	}

	cs, ok := agentConfig["channelStatus"].(map[string]any)
	if !ok {
		return
	}
	for chName, st := range cs {
		sm, ok := st.(map[string]any)
		if !ok {
			continue
		}
		if sm["connected"] == nil && channelActive[chName] {
			sm["connected"] = true
			if sm["health"] == nil || sm["health"] == "" || sm["health"] == false {
				sm["health"] = "active"
			}
		}
	}
}

func collectCrons(cronPath string, loc *time.Location) []map[string]any {
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

func buildSIDToKeyMap(stores []sessionStoreFile) map[string]string {
	sidToKey := map[string]string{}
	for _, sf := range stores {
		for k, v := range sf.store {
			sid, _ := v["sessionId"].(string)
			if sid != "" {
				if _, exists := sidToKey[sid]; !exists {
					sidToKey[sid] = k
				}
			}
		}
	}
	return sidToKey
}

func isSubagentSession(sessionKey, knownType string) bool {
	return strings.Contains(sessionKey, "subagent:") || knownType == "subagent"
}

func collectTokenUsage(
	basePath string, loc *time.Location,
	todayStr, date7d, date30d string,
	knownSIDs map[string]string, sidToKey map[string]string,
	modelAliases map[string]string,
	modelsAll, modelsToday, models7d, models30d map[string]*tokenBucket,
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*tokenBucket,
	dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int,
	dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64,
	dailySubagentCount map[string]int,
) []map[string]any {
	var subagentRuns []map[string]any

	// Collect both active and deleted JSONL files
	activePattern := filepath.Join(basePath, "*/sessions/*.jsonl")
	deletedPattern := filepath.Join(basePath, "*/sessions/*.jsonl.deleted.*")
	activeFiles, _ := filepath.Glob(activePattern)
	deletedFiles, _ := filepath.Glob(deletedPattern)
	allFiles := append(activeFiles, deletedFiles...)

	for _, f := range allFiles {
		sid := filepath.Base(f)
		sid = strings.Replace(sid, ".jsonl", "", 1)
		// Strip .deleted.* suffix
		if idx := strings.Index(sid, ".deleted."); idx >= 0 {
			sid = sid[:idx]
		}

		sessionKey := sidToKey[sid]
		isSubagent := isSubagentSession(sessionKey, knownSIDs[sid])

		var sessionCost float64
		var sessionModel string
		var sessionFirstTs, sessionLastTs time.Time
		sessionTask := sessionKey
		if sessionTask == "" && len(sid) > 12 {
			sessionTask = sid[:12]
		} else if sessionTask == "" {
			sessionTask = sid
		}

		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		for start := 0; start < len(data); {
			line := data[start:]
			if idx := bytes.IndexByte(line, '\n'); idx >= 0 {
				line = line[:idx]
				start += idx + 1
			} else {
				start = len(data)
			}
			if len(line) == 0 {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal(line, &obj); err != nil {
				continue
			}
			msg := asObj(obj["message"])
			if msg == nil {
				continue
			}
			role, _ := msg["role"].(string)
			if role != "assistant" {
				continue
			}
			usage := asObj(msg["usage"])
			if usage == nil {
				continue
			}
			tt, _ := usage["totalTokens"].(float64)
			if tt == 0 {
				continue
			}
			model, _ := msg["model"].(string)
			if model == "" {
				model = "unknown"
			}
			if strings.Contains(model, "delivery-mirror") {
				continue
			}

			name := modelName(model)
			var costTotal float64
			if costObj, ok := usage["cost"].(map[string]any); ok {
				if t, ok := costObj["total"].(float64); ok {
					costTotal = t
				}
			}
			if costTotal < 0 {
				costTotal = 0
			}

			inp, _ := usage["input"].(float64)
			out, _ := usage["output"].(float64)
			cr, _ := usage["cacheRead"].(float64)

			getBucket(modelsAll, name).add(int(inp), int(out), int(cr), int(tt), costTotal)

			if isSubagent {
				getBucket(subagentAll, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				sessionCost += costTotal
				sessionModel = name
			}

			ts, _ := obj["timestamp"].(string)
			var msgDate string
			if ts != "" {
				ts = strings.Replace(ts, "Z", "+00:00", 1)
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					t = t.In(loc)
					msgDate = t.Format("2006-01-02")
					if sessionFirstTs.IsZero() {
						sessionFirstTs = t
					}
					sessionLastTs = t
				}
			}

			if msgDate != "" {
				ensureMapMap(dailyCosts, msgDate)[name] += costTotal
				ensureMapMapInt(dailyTokens, msgDate)[name] += int(tt)
				ensureMapMapInt(dailyCalls, msgDate)[name]++
				if isSubagent {
					dailySubagentCosts[msgDate] += costTotal
				}
			}

			if msgDate == todayStr {
				getBucket(modelsToday, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				if isSubagent {
					getBucket(subagentToday, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				}
			}
			if msgDate >= date7d {
				getBucket(models7d, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				if isSubagent {
					getBucket(subagent7d, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				}
			}
			if msgDate >= date30d {
				getBucket(models30d, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				if isSubagent {
					getBucket(subagent30d, name).add(int(inp), int(out), int(cr), int(tt), costTotal)
				}
			}
		}

		if isSubagent && sessionCost > 0 && !sessionLastTs.IsZero() {
			var durationSec int
			if !sessionFirstTs.IsZero() {
				durationSec = int(sessionLastTs.Sub(sessionFirstTs).Seconds())
			}
			task := sessionTask
			if len(task) > 60 {
				task = task[:60]
			}
			subagentRuns = append(subagentRuns, map[string]any{
				"task":        task,
				"model":       sessionModel,
				"cost":        math.Round(sessionCost*10000) / 10000,
				"durationSec": durationSec,
				"status":      "completed",
				"timestamp":   sessionLastTs.Format("2006-01-02 15:04"),
				"date":        sessionLastTs.Format("2006-01-02"),
			})
		}
	}

	return subagentRuns
}

func buildDailyChart(now time.Time, dailyCosts map[string]map[string]float64,
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
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].cost > sorted[j].cost })
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
				d := entry["date"].(string)
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

func collectGitLog(openclawPath string) []map[string]any {
	var gitLog []map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", openclawPath, "log",
		"--oneline", "-5", "--format=%h|%s|%ar").Output()
	if err != nil {
		return gitLog
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
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

func buildAlerts(totalCostToday, costHigh, costWarn float64,
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

func buildCostBreakdown(m map[string]*tokenBucket) []map[string]any {
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
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].cost > pairs[j].cost })
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

func sumBucketCosts(m map[string]*tokenBucket) float64 {
	var total float64
	for _, b := range m {
		total += b.Cost
	}
	return total
}

func filterByDate(runs []map[string]any, targetDate, op string) []map[string]any {
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

func limitSlice[T any](s []T, max int) []T {
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
