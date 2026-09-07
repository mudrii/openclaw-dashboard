package apprefresh

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRuntimeUsageChartCombinesSharedDisplayNames(t *testing.T) {
	var usage runtimeUsage
	if err := json.Unmarshal([]byte(`{"totals":{"totalCost":5},"cacheStatus":{"status":"fresh"},"aggregates":{"daily":[{"date":"2026-09-07","cost":5}],"modelDaily":[{"date":"2026-09-07","provider":"first","model":"gpt-5.5","cost":2},{"date":"2026-09-07","provider":"second","model":"gpt-5.5","cost":3}]}}`), &usage); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{}
	applyRuntimeUsage(data, "30d", usage, time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC))
	chart := data["dailyChart"].([]map[string]any)
	day := chart[len(chart)-1]
	models := day["models"].(map[string]any)
	if models["GPT-5.5"] != float64(5) || day["total"] != float64(5) {
		t.Fatalf("model breakdown lost costs: %v", day)
	}
}

func TestRuntimeUsageCostBreakdown(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		complete   bool
	}{
		{"priced", `{"totals":{"totalCost":2},"aggregates":{"byModel":[{"provider":"p","model":"m","totals":{"totalCost":2}}]},"cacheStatus":{"status":"fresh"}}`, true},
		{"zero", `{"totals":{"totalCost":0},"aggregates":{"byModel":[{"model":"m","totals":{"totalCost":0}}]},"cacheStatus":{"status":"fresh"}}`, true},
		{"unpriced", `{"totals":{"totalCost":2,"missingCostEntries":1},"cacheStatus":{"status":"fresh"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var u runtimeUsage
			if err := json.Unmarshal([]byte(tc.body), &u); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"All", "Today"} {
				d := map[string]any{}
				applyRuntimeUsage(d, suffix, u, time.Now())
				key := "costBreakdown"
				if suffix == "Today" {
					key += suffix
				}
				rows, ok := d[key].([]map[string]any)
				if tc.complete && (!ok || len(rows) != 1 || rows[0]["cost"] != *u.Totals.TotalCost) {
					t.Fatalf("%s=%v", key, d[key])
				}
				if !tc.complete && d[key] != nil {
					t.Fatalf("incomplete breakdown=%v", d[key])
				}
			}
		})
	}
}

func TestCronDurationMissingVersusZero(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state map[string]any
		want  any
	}{
		{"missing", map[string]any{}, nil},
		{"null", map[string]any{"lastDurationMs": nil}, nil},
		{"zero", map[string]any{"lastDurationMs": float64(0)}, 0},
		{"duration", map[string]any{"lastDurationMs": float64(123)}, 123},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cronJobToMap(map[string]any{"state": tc.state}, nil, time.UTC)
			if got["lastDurationMs"] != tc.want {
				t.Fatalf("duration=%v, want %v", got["lastDurationMs"], tc.want)
			}
		})
	}
}

func TestRuntimeUsageAuthoritativeTotalsAndMissingPrices(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if method != "sessions.usage" || params["agentScope"] != "all" || params["timeZone"] != "Asia/Kuala_Lumpur" || params["limit"] != float64(1) {
			t.Fatalf("query: %s %v", method, params)
		}
		return `{"totals":{"input":90,"output":10,"totalTokens":100,"totalCost":0,"missingCostEntries":2,"missingCostByModel":{"p/m":2}},"sessions":[{"usage":{"totalTokens":1}}],"aggregates":{"byModel":[{"provider":"p","model":"m","count":2,"totals":{"input":90,"output":10,"totalTokens":100,"totalCost":0,"missingCostEntries":2}}],"daily":[{"date":"2026-09-05","tokens":100,"cost":0,"messages":2}],"modelDaily":[]},"cacheStatus":{"status":"fresh","pendingFiles":0,"staleFiles":0}}`
	})
	usage, err := collectRuntimeUsage(t.Context(), client, "all", "", "", "Asia/Kuala_Lumpur")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Totals.TotalTokens != 100 || usage.CompleteCost() || !usage.CompleteTokens() {
		t.Fatalf("usage=%+v", usage)
	}
	rows := usage.ModelRows()
	if len(rows) != 1 || rows[0]["cost"] != nil || rows[0]["totalTokensRaw"] != 100 {
		t.Fatalf("rows=%v", rows)
	}
	data := map[string]any{}
	applyRuntimeUsage(data, "All", usage, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	if data["totalCostAllTime"] != nil {
		t.Fatalf("unknown price rendered as zero: %v", data)
	}
}

func TestRuntimeUsageWarmingAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		wantErr, complete bool
	}{
		{"warming", `{"totals":{"totalTokens":4,"totalCost":0},"aggregates":{"byModel":[]},"cacheStatus":{"status":"refreshing","pendingFiles":1}}`, false, false},
		{"empty", `{"totals":{"totalTokens":0,"totalCost":0},"aggregates":{"byModel":[]},"cacheStatus":{"status":"fresh"}}`, false, true},
		{"missing", `{}`, true, false},
		{"unknown cache", `{"totals":{"totalTokens":4,"totalCost":0},"aggregates":{"byModel":[]}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := rpcFixtureClient(t, func(_ string, params map[string]any) string {
				if params["startDate"] != "2026-09-05" || params["endDate"] != "2026-09-05" || params["range"] != nil {
					t.Fatalf("date query=%v", params)
				}
				return tc.body
			})
			u, err := collectRuntimeUsage(t.Context(), client, "", "2026-09-05", "2026-09-05", "UTC")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if err == nil && u.CompleteTokens() != tc.complete {
				t.Fatalf("usage=%+v", u)
			}
		})
	}
}
