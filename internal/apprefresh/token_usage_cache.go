package apprefresh

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const tokenUsageCacheVersion = 1

type tokenUsageCache struct {
	Version int                              `json:"version"`
	Files   map[string]tokenUsageFileSummary `json:"files"`
}

type tokenUsageFileSummary struct {
	Size               int64                             `json:"size"`
	ModTimeUnixNano    int64                             `json:"mtimeNs"`
	Models             map[string]TokenBucket            `json:"models,omitempty"`
	Daily              map[string]map[string]TokenBucket `json:"daily,omitempty"`
	SessionCost        float64                           `json:"sessionCost,omitempty"`
	SessionModel       string                            `json:"sessionModel,omitempty"`
	SessionFirstUnixMs int64                             `json:"sessionFirstUnixMs,omitempty"`
	SessionLastUnixMs  int64                             `json:"sessionLastUnixMs,omitempty"`
}

func CollectTokenUsageWithCache(
	cachePath string,
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
	_ = modelAliases

	activePattern := filepath.Join(basePath, "*/sessions/*.jsonl")
	deletedPattern := filepath.Join(basePath, "*/sessions/*.jsonl.deleted.*")
	activeFiles, _ := filepath.Glob(activePattern)
	deletedFiles, _ := filepath.Glob(deletedPattern)
	allFiles := make([]string, 0, len(activeFiles)+len(deletedFiles))
	allFiles = append(allFiles, activeFiles...)
	allFiles = append(allFiles, deletedFiles...)
	sort.Strings(allFiles)

	cache := loadTokenUsageCache(cachePath)
	nextCache := tokenUsageCache{
		Version: tokenUsageCacheVersion,
		Files:   make(map[string]tokenUsageFileSummary, len(allFiles)),
	}
	subagentRuns := make([]map[string]any, 0)

	for _, path := range allFiles {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		summary, ok := cache.Files[path]
		if !ok || summary.Size != info.Size() || summary.ModTimeUnixNano != info.ModTime().UnixNano() {
			summary, err = parseTokenUsageFile(path, info, loc)
			if err != nil {
				log.Printf("[dashboard] token usage parse skipped for %s: %v", path, err)
				continue
			}
		}
		nextCache.Files[path] = summary
		applyTokenUsageSummary(path, summary, loc, todayStr, date7d, date30d, knownSIDs, sidToKey,
			modelsAll, modelsToday, models7d, models30d,
			subagentAll, subagentToday, subagent7d, subagent30d,
			dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
			&subagentRuns,
		)
	}

	saveTokenUsageCache(cachePath, nextCache)
	return subagentRuns
}

func loadTokenUsageCache(path string) tokenUsageCache {
	cache := tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}}
	if path == "" {
		return cache
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cache
	}
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != tokenUsageCacheVersion || cache.Files == nil {
		return tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}}
	}
	return cache
}

func saveTokenUsageCache(path string, cache tokenUsageCache) {
	if path == "" {
		return
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func parseTokenUsageFile(path string, info os.FileInfo, loc *time.Location) (tokenUsageFileSummary, error) {
	summary := tokenUsageFileSummary{
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
		Models:          map[string]TokenBucket{},
		Daily:           map[string]map[string]TokenBucket{},
	}

	fh, err := os.Open(path)
	if err != nil {
		return summary, err
	}
	defer fh.Close()

	reader := bufio.NewReaderSize(fh, 256*1024)
	var sessionFirstTs, sessionLastTs time.Time
	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var obj map[string]any
			if json.Unmarshal(line, &obj) == nil {
				msg := asObj(obj["message"])
				if msg != nil {
					role, _ := msg["role"].(string)
					if role == "assistant" {
						usage := asObj(msg["usage"])
						if usage != nil {
							tt, _ := usage["totalTokens"].(float64)
							if tt > 0 {
								model, _ := msg["model"].(string)
								if model == "" {
									model = "unknown"
								}
								if !strings.Contains(model, "delivery-mirror") {
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

									modelBucket := summary.Models[name]
									modelBucket.add(int(inp), int(out), int(cr), int(tt), costTotal)
									summary.Models[name] = modelBucket
									summary.SessionCost += costTotal
									summary.SessionModel = name

									ts, _ := obj["timestamp"].(string)
									if ts != "" {
										ts = strings.Replace(ts, "Z", "+00:00", 1)
										if t, err := time.Parse(time.RFC3339, ts); err == nil {
											t = t.In(loc)
											msgDate := t.Format("2006-01-02")
											if summary.Daily[msgDate] == nil {
												summary.Daily[msgDate] = map[string]TokenBucket{}
											}
											dailyBucket := summary.Daily[msgDate][name]
											dailyBucket.add(int(inp), int(out), int(cr), int(tt), costTotal)
											summary.Daily[msgDate][name] = dailyBucket
											if sessionFirstTs.IsZero() {
												sessionFirstTs = t
											}
											sessionLastTs = t
										}
									}
								}
							}
						}
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return summary, err
		}
	}

	if !sessionFirstTs.IsZero() {
		summary.SessionFirstUnixMs = sessionFirstTs.UnixMilli()
	}
	if !sessionLastTs.IsZero() {
		summary.SessionLastUnixMs = sessionLastTs.UnixMilli()
	}
	return summary, nil
}

func applyTokenUsageSummary(
	path string,
	summary tokenUsageFileSummary,
	loc *time.Location,
	todayStr, date7d, date30d string,
	knownSIDs map[string]string,
	sidToKey map[string]string,
	modelsAll, modelsToday, models7d, models30d map[string]*TokenBucket,
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*TokenBucket,
	dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int,
	dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64,
	dailySubagentCount map[string]int,
	subagentRuns *[]map[string]any,
) {
	_ = dailySubagentCount

	sid := filepath.Base(path)
	sid = strings.Replace(sid, ".jsonl", "", 1)
	if idx := strings.Index(sid, ".deleted."); idx >= 0 {
		sid = sid[:idx]
	}
	sessionKey := sidToKey[sid]
	isSubagent := isSubagentSession(sessionKey, knownSIDs[sid])

	for model, bucket := range summary.Models {
		getBucket(modelsAll, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
		if isSubagent {
			getBucket(subagentAll, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
		}
	}

	for date, perModel := range summary.Daily {
		for model, bucket := range perModel {
			ensureMapMap(dailyCosts, date)[model] += bucket.Cost
			ensureMapMapInt(dailyTokens, date)[model] += bucket.Total
			ensureMapMapInt(dailyCalls, date)[model] += bucket.Calls
			if date == todayStr {
				getBucket(modelsToday, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				if isSubagent {
					getBucket(subagentToday, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				}
			}
			if date >= date7d {
				getBucket(models7d, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				if isSubagent {
					getBucket(subagent7d, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				}
			}
			if date >= date30d {
				getBucket(models30d, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				if isSubagent {
					getBucket(subagent30d, model).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				}
			}
			if isSubagent {
				dailySubagentCosts[date] += bucket.Cost
			}
		}
	}

	if !isSubagent || summary.SessionCost <= 0 || summary.SessionLastUnixMs == 0 {
		return
	}

	sessionTask := sessionKey
	if sessionTask == "" && len(sid) > 12 {
		sessionTask = sid[:12]
	} else if sessionTask == "" {
		sessionTask = sid
	}
	if len(sessionTask) > 60 {
		sessionTask = sessionTask[:60]
	}

	lastTs := time.UnixMilli(summary.SessionLastUnixMs).In(loc)
	durationSec := 0
	if summary.SessionFirstUnixMs > 0 {
		durationSec = int(time.UnixMilli(summary.SessionLastUnixMs).Sub(time.UnixMilli(summary.SessionFirstUnixMs)).Seconds())
	}
	*subagentRuns = append(*subagentRuns, map[string]any{
		"task":        sessionTask,
		"model":       summary.SessionModel,
		"cost":        math.Round(summary.SessionCost*10000) / 10000,
		"durationSec": durationSec,
		"status":      "completed",
		"timestamp":   lastTs.Format("2006-01-02 15:04"),
		"date":        lastTs.Format("2006-01-02"),
	})
}
