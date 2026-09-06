package apprefresh

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestRuntimeSessionTokenReasons(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		tokens                  any
		fresh                   any
		limit                   any
		wantTokens, wantContext string
	}{
		{"absent", nil, nil, float64(128000), "not_reported", "tokens_not_reported"},
		{"stale", float64(12), false, float64(100), "stale", "tokens_stale"},
		{"zero", float64(0), true, float64(100), "ready", "ready"},
		{"no limit", float64(12), true, nil, "ready", "limit_not_reported"},
		{"invalid", float64(-1), true, float64(100), "invalid", "tokens_invalid"},
		{"nonfinite", math.NaN(), true, float64(100), "invalid", "tokens_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := runtimeSessionRow(map[string]any{"totalTokens": tc.tokens, "totalTokensFresh": tc.fresh, "contextTokens": tc.limit}, time.UTC, nil)
			if row["tokenState"] != tc.wantTokens || row["contextState"] != tc.wantContext {
				t.Fatalf("states = %v / %v", row["tokenState"], row["contextState"])
			}
			if tc.wantTokens != "ready" && row["totalTokens"] != nil {
				t.Fatal("unusable tokens displayed as current")
			}
		})
	}
}

func TestConfiguredPricingProjection(t *testing.T) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(`{"models":{"providers":{"custom":{"apiKey":"private-key","baseUrl":"private-url","models":[{"id":"m","cost":{"input":1,"output":2,"cacheRead":0,"cacheWrite":-1,"secret":"private-cost"}}]}}}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	_, _, _, _, out := parseOpenclawConfig(cfg, "/state/agents")
	prices, ok := out["modelPricing"].(map[string]any)
	if !ok {
		t.Fatal("missing pricing projection")
	}
	cost := asObj(prices["custom/m"])
	if cost["input"] != float64(1) || cost["cacheRead"] != float64(0) || cost["cacheWrite"] != nil {
		t.Fatalf("cost=%v", cost)
	}
	data, _ := json.Marshal(prices)
	if strings.Contains(string(data), "private-") {
		t.Fatal("pricing leaked private fields")
	}
}
