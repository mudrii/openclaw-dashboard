package apprefresh

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Equal costs must not leave output order to map iteration.
func TestCostSortsTieBreakByName(t *testing.T) {
	buckets := map[string]*TokenBucket{}
	for _, name := range []string{"delta", "alpha", "charlie", "bravo"} {
		buckets[name] = &TokenBucket{Cost: 1}
	}
	want := "alpha,bravo,charlie,delta"
	for range 20 {
		var list, breakdown []string
		for _, e := range BucketsToList(buckets) {
			list = append(list, e.Model)
		}
		for _, e := range BuildCostBreakdown(buckets) {
			breakdown = append(breakdown, e["model"].(string))
		}
		if strings.Join(list, ",") != want || strings.Join(breakdown, ",") != want {
			t.Fatalf("BucketsToList=%s BuildCostBreakdown=%s, want %s", list, breakdown, want)
		}
	}
}

// TestDailyChartTopModelsTieBreakByName pins which equal-cost models make the
// top-6 cut; the rest fold into "Other".
func TestDailyChartTopModelsTieBreakByName(t *testing.T) {
	now := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	day := now.Format(time.DateOnly)
	costs := map[string]float64{}
	for i := range 8 {
		costs[fmt.Sprintf("m%d", i)] = 1
	}
	for range 20 {
		chart := BuildDailyChart(now, map[string]map[string]float64{day: costs}, nil, nil, nil, nil, t.TempDir())
		models := chart[len(chart)-1]["models"].(map[string]any)
		for i := range 6 {
			if _, ok := models[fmt.Sprintf("m%d", i)]; !ok {
				t.Fatalf("top models = %v, want m0..m5 plus Other", models)
			}
		}
	}
}
