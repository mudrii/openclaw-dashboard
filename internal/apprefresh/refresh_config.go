package apprefresh

import (
	"fmt"
	"strings"
)

// defaultAgentConfig returns the empty agent-config skeleton served to the
// dashboard when openclaw.json is missing or unparseable.
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

// parseOpenclawConfig walks a parsed openclaw.json map and produces the
// dashboard-facing summary (compactionMode, skills, availableModels,
// model aliases, agentConfig). It is the single boundary where the loose
// JSON shape is translated into the dashboard's wire format.
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

	compaction, compactionMode := parseCompaction(defaults)
	skills = parseSkills(oc)
	primary, fallbacks, imageModel, models := parseModelDefaults(defaults)
	availableModels, modelAliases = parseAvailableModels(models, primary)

	channelsCfg := jsonObj(oc, "channels")
	tgCfg := jsonObj(channelsCfg, "telegram")
	channelsEnabled, channelStatus := parseChannels(channelsCfg)

	webCfg := jsonObj(jsonObj(jsonObj(oc, "tools"), "web"), "search")
	gwCfg := jsonObj(oc, "gateway")

	hooksList := parseHooks(oc)
	pluginsList := parsePlugins(oc)
	skillsCfg := parseSkillsCfg(oc)

	bindingsList := parseBindings(oc, agents)

	hasTTS := jsonStr(jsonObj(oc, "talk"), "apiKey") != ""

	diagEnabled := false
	if d, ok := jsonObj(oc, "diagnostics")["enabled"].(bool); ok {
		diagEnabled = d
	}

	modelParams := parseModelParams(models)
	availModels := parseAvailModelsList(models)
	agentEntries := parseAgents(agents, primary, fallbacks, modelAliases, modelParams)

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

func parseCompaction(defaults map[string]any) (map[string]any, string) {
	compaction := jsonObj(defaults, "compaction")
	mode, ok := compaction["mode"].(string)
	if !ok {
		mode = "auto"
	}
	return compaction, mode
}

func parseSkills(oc map[string]any) []map[string]any {
	entries := jsonObj(jsonObj(oc, "skills"), "entries")
	var out []map[string]any
	for _, name := range sortedJSONKeys(entries) {
		conf := entries[name]
		enabled := true
		if cm, ok := conf.(map[string]any); ok {
			if e, ok := cm["enabled"].(bool); ok {
				enabled = e
			}
		}
		out = append(out, map[string]any{"name": name, "active": enabled, "type": "builtin"})
	}
	return out
}

func parseModelDefaults(defaults map[string]any) (primary string, fallbacks []string, imageModel string, models map[string]any) {
	modelCfg := jsonObj(defaults, "model")
	primary = jsonStr(modelCfg, "primary")
	for _, f := range jsonArr(modelCfg, "fallbacks") {
		if s, ok := f.(string); ok {
			fallbacks = append(fallbacks, s)
		}
	}
	imageModel = jsonStr(jsonObj(defaults, "imageModel"), "primary")
	models = jsonObj(defaults, "models")
	return
}

func parseAvailableModels(models map[string]any, primary string) ([]map[string]any, map[string]string) {
	out := []map[string]any{}
	aliases := map[string]string{}
	for _, mid := range sortedJSONKeys(models) {
		mc := asObj(models[mid])
		alias := jsonStr(mc, "alias")
		if alias == "" {
			alias = mid
		}
		aliases[mid] = alias
		provider := "unknown"
		if strings.Contains(mid, "/") {
			provider = TitleCase(strings.SplitN(mid, "/", 2)[0])
		}
		status := "available"
		if mid == primary {
			status = "active"
		}
		out = append(out, map[string]any{
			"provider": provider, "name": alias, "id": mid, "status": status,
		})
	}
	return out, aliases
}

func parseChannels(channelsCfg map[string]any) ([]string, map[string]any) {
	var enabled []string
	status := map[string]any{}
	for _, chName := range sortedJSONKeys(channelsCfg) {
		cm := asObj(channelsCfg[chName])
		if cm == nil {
			continue
		}
		on := true
		if e, ok := cm["enabled"].(bool); ok {
			on = e
		}
		if on {
			enabled = append(enabled, chName)
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

		status[chName] = map[string]any{
			"enabled":    on,
			"configured": configured,
			"connected":  connected,
			"health":     health,
			"error":      errorMsg,
		}
	}
	return enabled, status
}

func parseHooks(oc map[string]any) []any {
	entries := jsonObj(jsonObj(jsonObj(oc, "hooks"), "internal"), "entries")
	var out []any
	for _, n := range sortedJSONKeys(entries) {
		hm := asObj(entries[n])
		enabled := true
		if hm != nil {
			if e, ok := hm["enabled"].(bool); ok {
				enabled = e
			}
		}
		out = append(out, map[string]any{"name": n, "enabled": enabled})
	}
	return out
}

func parsePlugins(oc map[string]any) []string {
	entries := jsonObj(jsonObj(oc, "plugins"), "entries")
	return append([]string(nil), sortedJSONKeys(entries)...)
}

func parseSkillsCfg(oc map[string]any) []any {
	entries := jsonObj(jsonObj(oc, "skills"), "entries")
	var out []any
	for _, n := range sortedJSONKeys(entries) {
		sm := asObj(entries[n])
		enabled := true
		if sm != nil {
			if e, ok := sm["enabled"].(bool); ok {
				enabled = e
			}
		}
		out = append(out, map[string]any{"name": n, "enabled": enabled})
	}
	return out
}

func parseBindings(oc, agents map[string]any) []any {
	var out []any
	for _, b := range jsonArr(oc, "bindings") {
		bm := asObj(b)
		if bm == nil {
			continue
		}
		match := asObj(bm["match"])
		peer := asObj(match["peer"])
		out = append(out, map[string]any{
			"agentId": jsonStr(bm, "agentId"),
			"channel": jsonStr(match, "channel"),
			"kind":    jsonStr(peer, "kind"),
			"id":      jsonStr(peer, "id"),
			"name":    "",
		})
	}

	// Default agent entry for unmatched channels.
	defaultAgent := "main"
	for _, a := range jsonArr(agents, "list") {
		am := asObj(a)
		if am == nil {
			continue
		}
		if d, ok := am["default"].(bool); ok && d {
			if id, ok := am["id"].(string); ok {
				defaultAgent = id
			}
		}
	}
	out = append(out, map[string]any{
		"agentId": defaultAgent, "channel": "all", "kind": "default",
		"id": "", "name": "All unmatched channels",
	})
	return out
}

func parseModelParams(models map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, mid := range sortedJSONKeys(models) {
		mc := asObj(models[mid])
		if mc == nil {
			continue
		}
		if p, ok := mc["params"].(map[string]any); ok {
			out[mid] = p
		}
	}
	return out
}

func parseAvailModelsList(models map[string]any) []any {
	var out []any
	for _, mid := range sortedJSONKeys(models) {
		mc := asObj(models[mid])
		alias := jsonStr(mc, "alias")
		if alias == "" {
			alias = mid
		}
		prov := "—"
		if strings.Contains(mid, "/") {
			prov = strings.SplitN(mid, "/", 2)[0]
		}
		out = append(out, map[string]any{
			"id": mid, "alias": alias, "provider": prov,
		})
	}
	return out
}

func parseAgents(agents map[string]any, primary string, fallbacks []string, modelAliases map[string]string, modelParams map[string]map[string]any) []any {
	agentList := jsonArr(agents, "list")
	var out []any
	if len(agentList) == 0 {
		params := modelParams[primary]
		var ctx1m any
		if params != nil {
			ctx1m = params["context1m"]
		}
		out = append(out, map[string]any{
			"id": "default", "role": "Default",
			"model": aliasOrID(modelAliases, primary), "modelId": primary,
			"workspace": "~/.openclaw/workspace", "isDefault": true,
			"context1m": ctx1m,
		})
		return out
	}

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
		out = append(out, map[string]any{
			"id": aid, "role": role,
			"model":     aliasOrID(modelAliases, amodel),
			"modelId":   amodel,
			"workspace": jsonStrDefault(am, "workspace", "~/.openclaw/workspace"),
			"isDefault": isDefault,
			"context1m": ctx1m,
			"fallbacks": fb,
		})
	}
	return out
}
