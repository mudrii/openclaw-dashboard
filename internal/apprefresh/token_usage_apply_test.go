package apprefresh

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// freshAggregates returns a complete set of empty aggregate maps for the
// CollectTokenUsage* family.
type tokenAggregates struct {
	modelsAll, modelsToday, models7d, models30d         map[string]*TokenBucket
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*TokenBucket
	dailyCosts                                          map[string]map[string]float64
	dailyTokens, dailyCalls                             map[string]map[string]int
	dailySubagentCosts                                  map[string]float64
	dailySubagentCount                                  map[string]int
}

func freshAggregates() tokenAggregates {
	return tokenAggregates{
		modelsAll:          map[string]*TokenBucket{},
		modelsToday:        map[string]*TokenBucket{},
		models7d:           map[string]*TokenBucket{},
		models30d:          map[string]*TokenBucket{},
		subagentAll:        map[string]*TokenBucket{},
		subagentToday:      map[string]*TokenBucket{},
		subagent7d:         map[string]*TokenBucket{},
		subagent30d:        map[string]*TokenBucket{},
		dailyCosts:         map[string]map[string]float64{},
		dailyTokens:        map[string]map[string]int{},
		dailyCalls:         map[string]map[string]int{},
		dailySubagentCosts: map[string]float64{},
		dailySubagentCount: map[string]int{},
	}
}

func writeJSONL(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCollectTokenUsage_Delegates verifies the non-cache wrapper aggregates the
// usage and writes NO cache file (it passes "" as cachePath internally).
func TestCollectTokenUsage_Delegates(t *testing.T) {
	tmp := t.TempDir()
	basePath := filepath.Join(tmp, "agents")
	writeJSONL(t, filepath.Join(basePath, "main", "sessions", "x.jsonl"),
		`{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":0,"cost":{"total":0.12}}}}`,
	)

	agg := freshAggregates()
	CollectTokenUsage(
		basePath, time.UTC, "2026-03-22", "2026-03-15", "2026-02-20",
		map[string]string{}, map[string]string{}, map[string]string{},
		agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
		agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
		agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
	)

	got := agg.modelsAll["GPT-5"]
	if got == nil || got.Total != 100 || got.Input != 60 || got.Output != 40 {
		t.Fatalf("expected aggregated GPT-5 bucket, got %+v", got)
	}

	// No cache file should be written anywhere under tmp.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != "agents" {
			t.Errorf("unexpected file written by non-cache wrapper: %q", name)
		}
	}
}

// TestApplyTokenUsageSummary_InclusiveWindows verifies the timezone-aligned
// inclusive date windows: today→today buckets, >=date7d→7d, the day before
// date7d lands in 30d but not 7d.
func TestApplyTokenUsageSummary_InclusiveWindows(t *testing.T) {
	loc := time.UTC
	todayStr := "2026-06-14"
	date7d := "2026-06-08"  // today - 6
	date30d := "2026-05-16" // today - 29

	dayBefore7d := "2026-06-07" // just outside the 7d window, inside 30d
	dayBefore30d := "2026-05-15"

	// modelsAll is populated from summary.Models, while the today/7d/30d windows
	// are populated from summary.Daily. Set both so we can assert each path.
	mk := func(date string) tokenUsageFileSummary {
		return tokenUsageFileSummary{
			Models: map[string]TokenBucket{"GPT-5": {Calls: 1, Total: 10, Cost: 1.0}},
			Daily: map[string]map[string]TokenBucket{
				date: {"GPT-5": {Calls: 1, Total: 10, Cost: 1.0}},
			},
		}
	}

	t.Run("today only in modelsToday", func(t *testing.T) {
		agg := freshAggregates()
		applyTokenUsageSummary("/x/main/sessions/s.jsonl", mk(todayStr), loc,
			todayStr, date7d, date30d, map[string]string{}, map[string]string{}, map[string]string{},
			agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
			agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
			agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
			new([]map[string]any))
		if agg.modelsToday["GPT-5"] == nil || agg.modelsToday["GPT-5"].Total != 10 {
			t.Errorf("today should populate modelsToday: %+v", agg.modelsToday["GPT-5"])
		}
		// today is also >= date7d and >= date30d.
		if agg.models7d["GPT-5"] == nil || agg.models30d["GPT-5"] == nil {
			t.Errorf("today should also fall in 7d and 30d windows")
		}
	})

	t.Run("date7d boundary inclusive in 7d", func(t *testing.T) {
		agg := freshAggregates()
		applyTokenUsageSummary("/x/main/sessions/s.jsonl", mk(date7d), loc,
			todayStr, date7d, date30d, map[string]string{}, map[string]string{}, map[string]string{},
			agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
			agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
			agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
			new([]map[string]any))
		if agg.models7d["GPT-5"] == nil {
			t.Errorf("date7d should be inclusive in 7d window")
		}
		if agg.modelsToday["GPT-5"] != nil {
			t.Errorf("date7d is not today, must not be in modelsToday")
		}
	})

	t.Run("day before date7d in 30d not 7d", func(t *testing.T) {
		agg := freshAggregates()
		applyTokenUsageSummary("/x/main/sessions/s.jsonl", mk(dayBefore7d), loc,
			todayStr, date7d, date30d, map[string]string{}, map[string]string{}, map[string]string{},
			agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
			agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
			agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
			new([]map[string]any))
		if agg.models7d["GPT-5"] != nil {
			t.Errorf("day before date7d must NOT be in 7d window")
		}
		if agg.models30d["GPT-5"] == nil {
			t.Errorf("day before date7d must be in 30d window")
		}
		if agg.modelsAll["GPT-5"] == nil {
			t.Errorf("any day populates modelsAll")
		}
	})

	t.Run("day before date30d only in modelsAll", func(t *testing.T) {
		agg := freshAggregates()
		applyTokenUsageSummary("/x/main/sessions/s.jsonl", mk(dayBefore30d), loc,
			todayStr, date7d, date30d, map[string]string{}, map[string]string{}, map[string]string{},
			agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
			agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
			agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
			new([]map[string]any))
		if agg.models30d["GPT-5"] != nil {
			t.Errorf("day before date30d must NOT be in 30d window")
		}
		if agg.modelsAll["GPT-5"] == nil || agg.modelsAll["GPT-5"].Total != 10 {
			t.Errorf("modelsAll still aggregates regardless of window")
		}
	})
}

// TestApplyTokenUsageSummary_SubagentRouting verifies subagent buckets mirror
// the model buckets and dailySubagentCosts is populated for subagent sessions.
func TestApplyTokenUsageSummary_SubagentRouting(t *testing.T) {
	loc := time.UTC
	todayStr := "2026-06-14"
	date7d := "2026-06-08"
	date30d := "2026-05-16"

	summary := tokenUsageFileSummary{
		Models:            map[string]TokenBucket{"GPT-5": {Calls: 2, Total: 50, Cost: 3.0}},
		Daily:             map[string]map[string]TokenBucket{todayStr: {"GPT-5": {Calls: 2, Total: 50, Cost: 3.0}}},
		SessionCost:       3.0,
		SessionModel:      "openai/gpt-5",
		SessionLastUnixMs: time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC).UnixMilli(),
	}

	// sidToKey maps the file's sid to a subagent: session key → isSubagent true.
	sid := "abcdef123456"
	sidToKey := map[string]string{sid: "agent:main:subagent:worker"}

	agg := freshAggregates()
	runs := new([]map[string]any)
	path := "/x/main/sessions/" + sid + ".jsonl"
	applyTokenUsageSummary(path, summary, loc,
		todayStr, date7d, date30d, map[string]string{}, sidToKey, map[string]string{},
		agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
		agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
		agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
		runs)

	// Model buckets and subagent buckets must both be populated and mirror.
	if agg.modelsAll["GPT-5"] == nil || agg.subagentAll["GPT-5"] == nil {
		t.Fatalf("subagent buckets should mirror model buckets")
	}
	if agg.subagentAll["GPT-5"].Total != agg.modelsAll["GPT-5"].Total {
		t.Errorf("subagentAll should mirror modelsAll total")
	}
	if agg.subagentToday["GPT-5"] == nil {
		t.Errorf("subagentToday should be populated for today's subagent usage")
	}
	if agg.dailySubagentCosts[todayStr] != 3.0 {
		t.Errorf("dailySubagentCosts[today] = %v, want 3.0", agg.dailySubagentCosts[todayStr])
	}
	if agg.dailySubagentCount[todayStr] != 1 {
		t.Errorf("dailySubagentCount[today] = %v, want 1", agg.dailySubagentCount[todayStr])
	}
	if len(*runs) != 1 {
		t.Fatalf("want one subagent run recorded, got %d", len(*runs))
	}
	run := (*runs)[0]
	if run["status"] != "completed" || run["model"] != "GPT-5" {
		t.Errorf("subagent run fields mismatch: %v", run)
	}
}

// TestApplyTokenUsageSummary_NonSubagentNoRouting confirms a non-subagent
// session does not populate subagent buckets or runs.
func TestApplyTokenUsageSummary_NonSubagentNoRouting(t *testing.T) {
	loc := time.UTC
	todayStr := "2026-06-14"

	summary := tokenUsageFileSummary{
		Models: map[string]TokenBucket{"GPT-5": {Total: 50, Cost: 3.0}},
		Daily:  map[string]map[string]TokenBucket{todayStr: {"GPT-5": {Total: 50, Cost: 3.0}}},
	}
	agg := freshAggregates()
	runs := new([]map[string]any)
	applyTokenUsageSummary("/x/main/sessions/plain.jsonl", summary, loc,
		todayStr, "2026-06-08", "2026-05-16", map[string]string{}, map[string]string{}, map[string]string{},
		agg.modelsAll, agg.modelsToday, agg.models7d, agg.models30d,
		agg.subagentAll, agg.subagentToday, agg.subagent7d, agg.subagent30d,
		agg.dailyCosts, agg.dailyTokens, agg.dailyCalls, agg.dailySubagentCosts, agg.dailySubagentCount,
		runs)

	if agg.subagentAll["GPT-5"] != nil {
		t.Errorf("non-subagent must not populate subagentAll")
	}
	if len(*runs) != 0 {
		t.Errorf("non-subagent must not record runs, got %d", len(*runs))
	}
	if len(agg.dailySubagentCosts) != 0 {
		t.Errorf("non-subagent must not populate dailySubagentCosts")
	}
}

// TestApplyTokenUsageSummary_LocLocalDateCrossesMidnight pins that the daily
// window date is computed from the parsed Daily map keys (which the parser
// derived in loc), and that an instant crossing midnight in a non-UTC location
// lands in the loc-local day. We exercise this through the file parser so the
// loc conversion is real.
func TestApplyTokenUsageSummary_LocLocalDateCrossesMidnight(t *testing.T) {
	// 2026-06-14T23:30:00Z is 2026-06-15 in a +02:00 zone.
	loc := time.FixedZone("plus2", 2*3600)
	tmp := t.TempDir()
	path := filepath.Join(tmp, "agents", "main", "sessions", "s.jsonl")
	writeJSONL(t, path,
		`{"timestamp":"2026-06-14T23:30:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":10,"input":5,"output":5,"cacheRead":0,"cost":{"total":1.0}}}}`,
	)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := parseTokenUsageFile(path, info, loc)
	if err != nil {
		t.Fatal(err)
	}
	// The loc-local date should be 2026-06-15, not 2026-06-14.
	if _, ok := summary.Daily["2026-06-15"]; !ok {
		t.Fatalf("expected loc-local date 2026-06-15 in Daily, got keys %v", keysOf(summary.Daily))
	}
	if _, ok := summary.Daily["2026-06-14"]; ok {
		t.Errorf("UTC date 2026-06-14 should NOT appear; loc conversion expected")
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
