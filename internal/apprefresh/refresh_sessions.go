package apprefresh

import (
	"cmp"
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type SessionStoreFile struct {
	AgentName string
	Store     map[string]map[string]any
}

func loadSessionStores(basePath string) []SessionStoreFile {
	var stores []SessionStoreFile
	pattern := filepath.Join(basePath, "*/sessions/sessions.json")
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Printf("[dashboard] loadSessionStores: skipping unreadable file %s: %v", f, err)
			continue
		}
		var store map[string]map[string]any
		if err := json.Unmarshal(data, &store); err != nil {
			log.Printf("[dashboard] loadSessionStores: skipping invalid JSON in %s: %v", f, err)
			continue
		}
		rel, _ := filepath.Rel(basePath, f)
		agentName := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		stores = append(stores, SessionStoreFile{AgentName: agentName, Store: store})
	}
	return stores
}

func buildGroupNames(stores []SessionStoreFile) map[string]string {
	groupNames := map[string]string{}
	for _, sf := range stores {
		for key, val := range sf.Store {
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
		if model, ok := readLastSessionModel(jsonlPath); ok {
			return model
		}
	}
	if m, ok := agentDefaults[agentName]; ok {
		return m
	}
	return "unknown"
}

func readLastSessionModel(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", false
	}

	const chunkSize int64 = 64 * 1024
	var tail string
	for end := info.Size(); end > 0; {
		start := max(end-chunkSize, 0)
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, start); err != nil {
			return "", false
		}

		chunk := string(buf) + tail
		lines := strings.Split(chunk, "\n")
		if start > 0 {
			tail = lines[0]
			lines = lines[1:]
		} else {
			tail = ""
		}

		for i := len(lines) - 1; i >= 0; i-- {
			if model, ok := sessionModelFromLine(lines[i]); ok {
				return model, true
			}
		}
		end = start
	}

	if model, ok := sessionModelFromLine(tail); ok {
		return model, true
	}
	return "", false
}

func sessionModelFromLine(line string) (string, bool) {
	text := strings.TrimSpace(line)
	if text == "" {
		return "", false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil || obj["type"] != "model_change" {
		return "", false
	}
	provider, _ := obj["provider"].(string)
	modelID, _ := obj["modelId"].(string)
	if provider == "" || modelID == "" {
		return "", false
	}
	return provider + "/" + modelID, true
}

func collectSessions(stores []SessionStoreFile, basePath string, loc *time.Location, now time.Time, todayStr string,
	modelAliases map[string]string, knownSIDs map[string]string,
	gateway map[string]any, liveModelTTL time.Duration) []map[string]any {
	_ = todayStr
	_ = gateway

	agentDefaults := loadAgentDefaultModels(basePath)

	gatewayModelMap := getLiveSessionModels(now, liveModelTTL)

	var sessionsList []map[string]any
	for _, sf := range stores {
		agentName := sf.AgentName
		for key, val := range sf.Store {
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

			displayName := TrimLabel(rawLabel)
			if displayName == "" {
				displayName = TrimLabel(subject)
			}
			if displayName == "" {
				displayName = TrimLabel(originLabel)
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
			switch {
			case gwModel != "":
				resolvedModel = gwModel
			case provOverride != "" && modelOverride != "":
				resolvedModel = provOverride + "/" + modelOverride
			default:
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

	slices.SortFunc(sessionsList, func(a, b map[string]any) int {
		ua, _ := a["updatedAt"].(float64)
		ub, _ := b["updatedAt"].(float64)
		return cmp.Compare(ub, ua)
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
