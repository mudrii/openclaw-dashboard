package apprefresh

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const tokenUsageCacheVersion = 2

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
	activePattern := filepath.Join(basePath, "*/sessions/*.jsonl")
	deletedPattern := filepath.Join(basePath, "*/sessions/*.jsonl.deleted.*")
	activeFiles, _ := filepath.Glob(activePattern)
	deletedFiles, _ := filepath.Glob(deletedPattern)
	allFiles := make([]string, 0, len(activeFiles)+len(deletedFiles))
	allFiles = append(allFiles, activeFiles...)
	allFiles = append(allFiles, deletedFiles...)
	slices.Sort(allFiles)

	cache := loadTokenUsageCache(cachePath)
	nextCache := tokenUsageCache{
		Version: tokenUsageCacheVersion,
		Files:   make(map[string]tokenUsageFileSummary, len(allFiles)),
	}
	// Pre-size for typical batch (~64 subagent sessions) to avoid early grow.
	subagentRuns := make([]map[string]any, 0, 64)

	for _, path := range allFiles {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		summary, ok := cache.Files[path]
		if !ok || summary.Size != info.Size() || summary.ModTimeUnixNano != info.ModTime().UnixNano() {
			summary, err = parseTokenUsageFile(path, info, loc)
			if err != nil {
				slog.Warn("[dashboard] token usage parse skipped", "path", path, "error", err)
				continue
			}
		}
		nextCache.Files[path] = summary
		applyTokenUsageSummary(path, summary, loc, todayStr, date7d, date30d, knownSIDs, sidToKey,
			modelAliases,
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
	fresh := tokenUsageCache{Version: tokenUsageCacheVersion, Files: map[string]tokenUsageFileSummary{}}
	if err := json.Unmarshal(data, &cache); err != nil {
		// Corrupt cache: recompute from scratch. Surfaced so a recurring parse
		// failure (disk corruption, truncated write) is visible, not silent.
		slog.Warn("[dashboard] loadTokenUsageCache: corrupt cache, recomputing", "path", path, "error", err)
		return fresh
	}
	if cache.Version != tokenUsageCacheVersion {
		// Schema evolved: discarding is correct, but log it so a version bump
		// that unexpectedly invalidates every cache on rollout is observable.
		slog.Warn("[dashboard] loadTokenUsageCache: version mismatch, recomputing",
			"path", path, "got", cache.Version, "want", tokenUsageCacheVersion)
		return fresh
	}
	if cache.Files == nil {
		slog.Warn("[dashboard] loadTokenUsageCache: nil files map, recomputing", "path", path)
		return fresh
	}
	return cache
}

func saveTokenUsageCache(path string, cache tokenUsageCache) {
	if path == "" {
		return
	}
	data, err := json.Marshal(cache)
	if err != nil {
		slog.Error("[dashboard] saveTokenUsageCache: marshal failed", "error", err)
		return
	}
	tmp := path + ".tmp"
	// 0o600 matches data.json + plist + systemd unit writers; cache holds
	// per-session token/cost data that should not be world-readable.
	// writeFileSync fsyncs before the rename below for durability parity with
	// the data.json writer.
	if err := writeFileSync(tmp, data, 0o600); err != nil {
		slog.Error("[dashboard] saveTokenUsageCache: write failed", "path", tmp, "error", err)
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		slog.Error("[dashboard] saveTokenUsageCache: rename failed", "from", tmp, "to", path, "error", err)
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
	defer func() { _ = fh.Close() }()

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

									modelBucket := summary.Models[model]
									modelBucket.add(int(inp), int(out), int(cr), int(tt), costTotal)
									summary.Models[model] = modelBucket
									summary.SessionCost += costTotal
									summary.SessionModel = model

									ts, _ := obj["timestamp"].(string)
									if ts != "" {
										if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
											t = t.In(loc)
											msgDate := t.Format("2006-01-02")
											if summary.Daily[msgDate] == nil {
												summary.Daily[msgDate] = map[string]TokenBucket{}
											}
											dailyBucket := summary.Daily[msgDate][model]
											dailyBucket.add(int(inp), int(out), int(cr), int(tt), costTotal)
											summary.Daily[msgDate][model] = dailyBucket
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
			// Return zero-value summary so a partial result cannot be persisted
			// to the cache by a future caller that ignores the error. The caller
			// at CollectTokenUsageWithCache already `continue`s on err, but
			// hardening this here prevents accidental cache poisoning if the
			// contract is ever broken.
			return tokenUsageFileSummary{
				Size:            info.Size(),
				ModTimeUnixNano: info.ModTime().UnixNano(),
				Models:          map[string]TokenBucket{},
				Daily:           map[string]map[string]TokenBucket{},
			}, err
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
	modelAliases map[string]string,
	modelsAll, modelsToday, models7d, models30d map[string]*TokenBucket,
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*TokenBucket,
	dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int,
	dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64,
	dailySubagentCount map[string]int,
	subagentRuns *[]map[string]any,
) {
	sid := filepath.Base(path)
	if idx := strings.Index(sid, ".deleted."); idx >= 0 {
		sid = sid[:idx]
	}
	sid = strings.TrimSuffix(sid, ".jsonl")
	sessionKey := sidToKey[sid]
	isSubagent := isSubagentSession(sessionKey, knownSIDs[sid])

	for model, bucket := range summary.Models {
		displayModel := resolveUsageModel(model, modelAliases)
		getBucket(modelsAll, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
		if isSubagent {
			getBucket(subagentAll, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
		}
	}

	for date, perModel := range summary.Daily {
		for model, bucket := range perModel {
			displayModel := resolveUsageModel(model, modelAliases)
			ensureMapMap(dailyCosts, date)[displayModel] += bucket.Cost
			ensureMapMapInt(dailyTokens, date)[displayModel] += bucket.Total
			ensureMapMapInt(dailyCalls, date)[displayModel] += bucket.Calls
			if date == todayStr {
				getBucket(modelsToday, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				if isSubagent {
					getBucket(subagentToday, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				}
			}
			if date >= date7d {
				getBucket(models7d, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				if isSubagent {
					getBucket(subagent7d, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				}
			}
			if date >= date30d {
				getBucket(models30d, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				if isSubagent {
					getBucket(subagent30d, displayModel).add(bucket.Input, bucket.Output, bucket.CacheRead, bucket.Total, bucket.Cost)
				}
			}
			if isSubagent {
				dailySubagentCosts[date] += bucket.Cost
			}
		}
	}

	if !isSubagent || summary.SessionLastUnixMs == 0 {
		return
	}

	sessionTask := sessionKey
	if sessionTask == "" && len(sid) > 12 {
		sessionTask = sid[:12]
	} else if sessionTask == "" {
		sessionTask = sid
	}
	sessionTask = truncateRunes(sessionTask, 60)

	lastTs := time.UnixMilli(summary.SessionLastUnixMs).In(loc)
	lastDate := lastTs.Format("2006-01-02")
	dailySubagentCount[lastDate]++
	durationSec := 0
	if summary.SessionFirstUnixMs > 0 {
		durationSec = int(time.UnixMilli(summary.SessionLastUnixMs).Sub(time.UnixMilli(summary.SessionFirstUnixMs)).Seconds())
	}
	*subagentRuns = append(*subagentRuns, map[string]any{
		"task":        sessionTask,
		"model":       resolveUsageModel(summary.SessionModel, modelAliases),
		"cost":        math.Round(summary.SessionCost*10000) / 10000,
		"durationSec": durationSec,
		"status":      "completed",
		"timestamp":   lastTs.Format("2006-01-02 15:04"),
		"date":        lastDate,
	})
}

func resolveUsageModel(model string, modelAliases map[string]string) string {
	displayModel := aliasOrID(modelAliases, model)
	if displayModel == model {
		return ModelName(model)
	}
	return displayModel
}
