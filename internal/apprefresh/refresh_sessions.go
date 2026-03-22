package apprefresh

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
			continue
		}
		var store map[string]map[string]any
		if err := json.Unmarshal(data, &store); err != nil {
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

func collectSessions(stores []SessionStoreFile, basePath string, loc *time.Location, now time.Time, todayStr string,
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
