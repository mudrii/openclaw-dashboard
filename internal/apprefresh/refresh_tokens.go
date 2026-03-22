package apprefresh

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TokenBucket struct {
	Calls     int     `json:"calls"`
	Input     int     `json:"input"`
	Output    int     `json:"output"`
	CacheRead int     `json:"cacheRead"`
	Total     int     `json:"totalTokens"`
	Cost      float64 `json:"cost"`
}

func (b *TokenBucket) add(inp, out, cr, tt int, cost float64) {
	b.Calls++
	b.Input += inp
	b.Output += out
	b.CacheRead += cr
	b.Total += tt
	b.Cost += cost
}

type TokenUsageEntry struct {
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

func BucketsToList(m map[string]*TokenBucket) []TokenUsageEntry {
	type kv struct {
		k string
		v *TokenBucket
	}
	var pairs []kv
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v.Cost > pairs[j].v.Cost })

	out := make([]TokenUsageEntry, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, TokenUsageEntry{
			Model:          p.k,
			Calls:          p.v.Calls,
			Input:          FmtTokens(p.v.Input),
			Output:         FmtTokens(p.v.Output),
			CacheRead:      FmtTokens(p.v.CacheRead),
			TotalTokens:    FmtTokens(p.v.Total),
			Cost:           math.Round(p.v.Cost*100) / 100,
			InputRaw:       p.v.Input,
			OutputRaw:      p.v.Output,
			CacheReadRaw:   p.v.CacheRead,
			TotalTokensRaw: p.v.Total,
		})
	}
	return out
}

func FmtTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return strconv.Itoa(n)
}

func getBucket(m map[string]*TokenBucket, key string) *TokenBucket {
	b, ok := m[key]
	if !ok {
		b = &TokenBucket{}
		m[key] = b
	}
	return b
}

func BuildSIDToKeyMap(stores []SessionStoreFile) map[string]string {
	sidToKey := map[string]string{}
	for _, sf := range stores {
		for k, v := range sf.Store {
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

func CollectTokenUsage(
	basePath string, loc *time.Location,
	todayStr, date7d, date30d string,
	knownSIDs map[string]string, sidToKey map[string]string,
	modelAliases map[string]string,
	modelsAll, modelsToday, models7d, models30d map[string]*TokenBucket,
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*TokenBucket,
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
	allFiles := make([]string, 0, len(activeFiles)+len(deletedFiles))
	allFiles = append(allFiles, activeFiles...)
	allFiles = append(allFiles, deletedFiles...)

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

		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(fh)
		scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
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

			name := ModelName(model)
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
		fh.Close()

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
