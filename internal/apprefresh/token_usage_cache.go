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

const tokenUsageCacheVersion = 3

type tokenUsageCache struct {
	Version int `json:"version"`
	// Location is the timezone the Daily buckets were computed in; a cache
	// built under a different zone is discarded because its dates are wrong.
	Location string                           `json:"location,omitempty"`
	Files    map[string]tokenUsageFileSummary `json:"files"`
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
	allFiles = canonicalLegacyUsageFiles(allFiles)

	cache := loadTokenUsageCache(cachePath)
	if cache.Location != loc.String() && len(cache.Files) > 0 {
		slog.Info("[dashboard] token usage cache timezone changed, recomputing",
			"path", cachePath, "got", cache.Location, "want", loc.String())
		cache.Files = map[string]tokenUsageFileSummary{}
	}
	nextCache := tokenUsageCache{
		Version:  tokenUsageCacheVersion,
		Location: loc.String(),
		Files:    make(map[string]tokenUsageFileSummary, len(allFiles)),
	}
	// Pre-size for typical batch (~64 subagent sessions) to avoid early grow.
	subagentRuns := make([]map[string]any, 0, 64)
	var parser tokenUsageParser
	parsed := 0

	for _, path := range allFiles {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		summary, ok := cache.Files[path]
		if !ok || summary.Size != info.Size() || summary.ModTimeUnixNano != info.ModTime().UnixNano() {
			parsed++
			summary, err = parser.parseFile(path, info, loc)
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

	// Every entry of nextCache is either freshly parsed or copied from the
	// loaded cache, so with no parses and equal sizes the loaded cache (same
	// zone) already holds exactly nextCache: skip the marshal and fsync.
	if parsed > 0 || cache.Location != nextCache.Location || len(cache.Files) != len(nextCache.Files) {
		saveTokenUsageCache(cachePath, nextCache)
	}
	return subagentRuns
}

// Recovery copies describe one session lineage, not additional usage. Prefer
// its active transcript; when only dated deleted copies survive, use the last
// one. Keep agents separate even when their session filenames coincide.
func canonicalLegacyUsageFiles(files []string) []string {
	selected := map[string]string{}
	for _, path := range files {
		key := path
		if i := strings.Index(path, ".jsonl.deleted."); i >= 0 {
			key = path[:i+len(".jsonl")]
		}
		old, ok := selected[key]
		if !ok || path == key || (old != key && path > old) {
			selected[key] = path
		}
	}
	result := make([]string, 0, len(selected))
	for _, path := range selected {
		result = append(result, path)
	}
	slices.Sort(result)
	return result
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
	if err := writeSnapshotAtomic(path, data); err != nil {
		slog.Error("[dashboard] saveTokenUsageCache: write failed", "path", path, "error", err)
	}
}

func parseTokenUsageFile(path string, info os.FileInfo, loc *time.Location) (tokenUsageFileSummary, error) {
	var p tokenUsageParser
	return p.parseFile(path, info, loc)
}

// tokenUsageParser parses transcripts one after another, reusing its read
// buffer, line decoder and string caches across files.
type tokenUsageParser struct {
	reader *bufio.Reader
	long   []byte // assembles lines longer than the reader's buffer
	lines  usageLineDecoder

	lastModel string // most recent model, reused while it repeats
	lastDay   struct {
		year  int
		month time.Month
		day   int
		key   string
	}
}

// readLine returns the next line including its '\n', like ReadBytes, but
// without copying it; the slice is only valid until the next call.
func (p *tokenUsageParser) readLine() ([]byte, error) {
	line, err := p.reader.ReadSlice('\n')
	if !errors.Is(err, bufio.ErrBufferFull) {
		return line, err
	}
	p.long = append(p.long[:0], line...)
	for errors.Is(err, bufio.ErrBufferFull) {
		line, err = p.reader.ReadSlice('\n')
		p.long = append(p.long, line...)
	}
	return p.long, err
}

// model returns b as a string, reusing the previous allocation while a
// session keeps using the same model.
func (p *tokenUsageParser) model(b []byte) string {
	if len(b) == 0 {
		return "unknown"
	}
	if string(b) != p.lastModel {
		p.lastModel = string(b)
	}
	return p.lastModel
}

// dateKey formats t as a YYYY-MM-DD bucket key, reusing the previous key
// for consecutive messages on the same day.
func (p *tokenUsageParser) dateKey(t time.Time) string {
	y, m, d := t.Date()
	if p.lastDay.key == "" || y != p.lastDay.year || m != p.lastDay.month || d != p.lastDay.day {
		p.lastDay.year, p.lastDay.month, p.lastDay.day = y, m, d
		p.lastDay.key = t.Format("2006-01-02")
	}
	return p.lastDay.key
}

func (p *tokenUsageParser) parseFile(path string, info os.FileInfo, loc *time.Location) (tokenUsageFileSummary, error) {
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

	if p.reader == nil {
		p.reader = bufio.NewReaderSize(fh, 256*1024)
	} else {
		p.reader.Reset(fh)
	}
	var sessionFirstTs, sessionLastTs time.Time
	for {
		line, err := p.readLine()
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			ev, valid := p.lines.decode(line)
			usage, ok := ev.usage()
			if valid && ok && usage.total > 0 && !bytes.Contains(ev.model, []byte("delivery-mirror")) {
				model := p.model(ev.model)
				costTotal := usageCost(usage.costTotal)
				inp, out, cr, cw, tt := usageTokens(usage.input), usageTokens(usage.output), usageTokens(usage.read), usageTokens(usage.write), usageTokens(usage.total)

				modelBucket := summary.Models[model]
				modelBucket.add(inp, out, cr, cw, tt, costTotal)
				summary.Models[model] = modelBucket
				summary.SessionCost += costTotal
				summary.SessionModel = model

				if len(ev.timestamp) > 0 {
					if t, err := time.Parse(time.RFC3339Nano, string(ev.timestamp)); err == nil {
						t = t.In(loc)
						msgDate := p.dateKey(t)
						if summary.Daily[msgDate] == nil {
							summary.Daily[msgDate] = map[string]TokenBucket{}
						}
						dailyBucket := summary.Daily[msgDate][model]
						dailyBucket.add(inp, out, cr, cw, tt, costTotal)
						summary.Daily[msgDate][model] = dailyBucket
						if sessionFirstTs.IsZero() {
							sessionFirstTs = t
						}
						sessionLastTs = t
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
		getBucket(modelsAll, displayModel).merge(bucket)
		if isSubagent {
			getBucket(subagentAll, displayModel).merge(bucket)
		}
	}

	for date, perModel := range summary.Daily {
		for model, bucket := range perModel {
			displayModel := resolveUsageModel(model, modelAliases)
			ensureMapMap(dailyCosts, date)[displayModel] += bucket.Cost
			ensureMapMapInt(dailyTokens, date)[displayModel] += bucket.Total
			ensureMapMapInt(dailyCalls, date)[displayModel] += bucket.Calls
			if date == todayStr {
				getBucket(modelsToday, displayModel).merge(bucket)
				if isSubagent {
					getBucket(subagentToday, displayModel).merge(bucket)
				}
			}
			if date >= date7d {
				getBucket(models7d, displayModel).merge(bucket)
				if isSubagent {
					getBucket(subagent7d, displayModel).merge(bucket)
				}
			}
			if date >= date30d {
				getBucket(models30d, displayModel).merge(bucket)
				if isSubagent {
					getBucket(subagent30d, displayModel).merge(bucket)
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
	// Resolve the alias, then prettify through ModelName so the Token Usage
	// panel matches the Sessions panel (e.g. "GLM-5.2", not a raw "glm-5.2"
	// alias); a genuine custom alias passes through ModelName's default arm.
	return ModelName(aliasOrID(modelAliases, model))
}

// Per-event caps for session usage values. Session JSONL is written by the
// OpenClaw runtime but can be corrupt or hand-edited; without the caps one
// absurd value overflows the int totals or sums the cost to +Inf, which
// json.Marshal rejects, failing the token cache and data.json writes.
const (
	maxUsageEventTokens = 1 << 40
	maxUsageEventCost   = 1e9
)

// usageTokens converts a decoded token count to an int in
// [0, maxUsageEventTokens]; negatives and NaN count as zero.
func usageTokens(f float64) int {
	if !(f > 0) {
		return 0
	}
	return int(min(f, maxUsageEventTokens))
}

// usageCost clamps a decoded cost to [0, maxUsageEventCost]; negatives and
// NaN count as zero.
func usageCost(f float64) float64 {
	if !(f > 0) {
		return 0
	}
	return min(f, maxUsageEventCost)
}
