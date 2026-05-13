package apprefresh

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

type TokenBucket struct {
	Calls     int     `json:"calls,omitempty"`
	Input     int     `json:"input,omitempty"`
	Output    int     `json:"output,omitempty"`
	CacheRead int     `json:"cacheRead,omitempty"`
	Total     int     `json:"totalTokens,omitempty"`
	Cost      float64 `json:"cost,omitempty"`
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
	Calls          int     `json:"calls,omitempty"`
	Input          string  `json:"input"`
	Output         string  `json:"output"`
	CacheRead      string  `json:"cacheRead"`
	TotalTokens    string  `json:"totalTokens"`
	Cost           float64 `json:"cost,omitempty"`
	InputRaw       int     `json:"inputRaw,omitempty"`
	OutputRaw      int     `json:"outputRaw,omitempty"`
	CacheReadRaw   int     `json:"cacheReadRaw,omitempty"`
	TotalTokensRaw int     `json:"totalTokensRaw,omitempty"`
}

func BucketsToList(m map[string]*TokenBucket) []TokenUsageEntry {
	type kv struct {
		k string
		v *TokenBucket
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	slices.SortFunc(pairs, func(a, b kv) int { return cmp.Compare(b.v.Cost, a.v.Cost) })

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
	return CollectTokenUsageWithCache(
		"",
		basePath, loc, todayStr, date7d, date30d,
		knownSIDs, sidToKey, modelAliases,
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)
}
