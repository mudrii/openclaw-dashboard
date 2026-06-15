package apprefresh

import (
	"slices"
	"testing"
)

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want string
	}{
		{"zero", 0, "0"},
		{"sub-thousand", 999, "999"},
		{"thousand boundary", 1000, "1.0K"},
		// 999_999 is below the 1_000_000 cutoff, so it stays in the K branch
		// and renders as "1000.0K" rather than rolling over to "1.0M".
		{"just below million", 999_999, "1000.0K"},
		{"million boundary", 1_000_000, "1.0M"},
		{"million and a quarter", 1_250_000, "1.2M"},
		// Negative values are below every threshold, so they pass through
		// strconv.Itoa unchanged (no humanization).
		{"negative small", -5, "-5"},
		{"negative large", -5000, "-5000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := FmtTokens(tc.in); got != tc.want {
				t.Errorf("FmtTokens(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBucketsToList(t *testing.T) {
	t.Run("empty map yields empty list", func(t *testing.T) {
		got := BucketsToList(map[string]*TokenBucket{})
		if len(got) != 0 {
			t.Fatalf("want empty, got %v", got)
		}
	})

	t.Run("cost-descending order and field mapping", func(t *testing.T) {
		m := map[string]*TokenBucket{
			"low":  {Calls: 1, Input: 1500, Output: 500, CacheRead: 2000, Total: 4000, Cost: 0.5},
			"high": {Calls: 3, Input: 2_000_000, Output: 10, CacheRead: 0, Total: 2_000_010, Cost: 12.345},
			"mid":  {Calls: 2, Input: 100, Output: 100, CacheRead: 100, Total: 300, Cost: 5.0},
		}
		got := BucketsToList(m)
		if len(got) != 3 {
			t.Fatalf("want 3 entries, got %d", len(got))
		}
		wantOrder := []string{"high", "mid", "low"}
		for i, w := range wantOrder {
			if got[i].Model != w {
				t.Errorf("order[%d] = %q, want %q", i, got[i].Model, w)
			}
		}

		// Field mapping on the top entry: raw ints preserved, strings humanized,
		// cost rounded to 2dp (12.345 → 12.35).
		top := got[0]
		if top.Calls != 3 {
			t.Errorf("Calls = %d, want 3", top.Calls)
		}
		if top.InputRaw != 2_000_000 || top.OutputRaw != 10 || top.CacheReadRaw != 0 || top.TotalTokensRaw != 2_000_010 {
			t.Errorf("raw fields mismatch: %+v", top)
		}
		if top.Input != "2.0M" || top.Output != "10" || top.CacheRead != "0" || top.TotalTokens != "2.0M" {
			t.Errorf("formatted fields mismatch: input=%q output=%q cacheRead=%q total=%q",
				top.Input, top.Output, top.CacheRead, top.TotalTokens)
		}
		if top.Cost != 12.35 {
			t.Errorf("Cost = %v, want 12.35 (2dp round)", top.Cost)
		}
	})

	t.Run("equal-cost ties: assert set not order", func(t *testing.T) {
		// SortFunc is not stable; for equal costs the relative order is
		// unspecified, so assert the set of models, not their positions.
		m := map[string]*TokenBucket{
			"a": {Cost: 1.0, Total: 10},
			"b": {Cost: 1.0, Total: 20},
			"c": {Cost: 1.0, Total: 30},
		}
		got := BucketsToList(m)
		gotModels := make([]string, len(got))
		for i, e := range got {
			gotModels[i] = e.Model
		}
		slices.Sort(gotModels)
		want := []string{"a", "b", "c"}
		if !slices.Equal(gotModels, want) {
			t.Errorf("model set = %v, want %v", gotModels, want)
		}
	})
}

func TestRound2(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		// 1.005*100 = 100.49999... in float64, so Round gives 100 → 1.0.
		{"binary float rounds down", 1.005, 1.0},
		{"normal round up", 2.675, 2.68},
		{"already 2dp", 3.14, 3.14},
		{"zero", 0, 0},
		{"negative", -1.235, -1.24},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := round2(tc.in); got != tc.want {
				t.Errorf("round2(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestSumBucketCosts(t *testing.T) {
	t.Run("empty map sums to zero", func(t *testing.T) {
		if got := sumBucketCosts(map[string]*TokenBucket{}); got != 0 {
			t.Errorf("want 0, got %v", got)
		}
	})

	t.Run("returns unrounded float sum", func(t *testing.T) {
		// 0.1 + 0.2 = 0.30000000000000004 in IEEE-754; sumBucketCosts does NOT
		// round. Build the costs from runtime values so the float error is not
		// constant-folded away, then compare against an identically-computed sum.
		a, b := 0.1, 0.2
		m := map[string]*TokenBucket{"a": {Cost: a}, "b": {Cost: b}}
		got := sumBucketCosts(m)
		if got != a+b {
			t.Errorf("want %v (unrounded), got %v", a+b, got)
		}
		// Confirm the accumulated value is the IEEE-754 result, not a rounded 0.3.
		if round2(got) != 0.3 {
			t.Errorf("round2 of sum should be 0.3, got %v", round2(got))
		}
	})

	t.Run("multiple integral costs", func(t *testing.T) {
		m := map[string]*TokenBucket{"a": {Cost: 1.5}, "b": {Cost: 2.5}, "c": {Cost: 6.0}}
		if got := sumBucketCosts(m); got != 10.0 {
			t.Errorf("want 10.0, got %v", got)
		}
	})
}

func TestBuildSIDToKeyMap(t *testing.T) {
	t.Run("empty stores yields empty map", func(t *testing.T) {
		got := BuildSIDToKeyMap(nil)
		if len(got) != 0 {
			t.Fatalf("want empty, got %v", got)
		}
	})

	t.Run("skips entries with empty sessionId", func(t *testing.T) {
		stores := []SessionStoreFile{{
			AgentName: "main",
			Store: map[string]map[string]any{
				"key-with-sid": {"sessionId": "sid-1"},
				"key-empty":    {"sessionId": ""},
				"key-missing":  {"other": "x"},
			},
		}}
		got := BuildSIDToKeyMap(stores)
		if len(got) != 1 || got["sid-1"] != "key-with-sid" {
			t.Fatalf("want only sid-1→key-with-sid, got %v", got)
		}
	})

	t.Run("skips non-string sessionId", func(t *testing.T) {
		stores := []SessionStoreFile{{
			AgentName: "main",
			Store: map[string]map[string]any{
				"numeric-sid": {"sessionId": 12345.0},
				"good":        {"sessionId": "sid-ok"},
			},
		}}
		got := BuildSIDToKeyMap(stores)
		if len(got) != 1 || got["sid-ok"] != "good" {
			t.Fatalf("want only sid-ok, got %v", got)
		}
	})

	t.Run("first-wins on duplicate sessionId across stores", func(t *testing.T) {
		// Duplicate sid appears in two separate store files; the first one
		// encountered (slice order, then map iteration) wins. Put the dups in
		// distinct stores so slice order is deterministic.
		stores := []SessionStoreFile{
			{AgentName: "first", Store: map[string]map[string]any{
				"key-A": {"sessionId": "dup"},
			}},
			{AgentName: "second", Store: map[string]map[string]any{
				"key-B": {"sessionId": "dup"},
			}},
		}
		got := BuildSIDToKeyMap(stores)
		if got["dup"] != "key-A" {
			t.Fatalf("want first-wins key-A, got %q", got["dup"])
		}
	})
}
