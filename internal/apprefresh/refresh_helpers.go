package apprefresh

import (
	"math"
	"slices"
	"unicode/utf8"
)

// JSON helper functions ---------------------------------------------------

// jsonObj returns the map under key, or nil. Callers must tolerate nil; nil
// values short-circuit nested calls like jsonObj(jsonObj(m, "a"), "b") because
// the inner nil propagates through subsequent helpers.
func jsonObj(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func jsonArr(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

func jsonStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func jsonStrDefault(m map[string]any, key, def string) string {
	s := jsonStr(m, key)
	if s == "" {
		return def
	}
	return s
}

func asObj(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func stringSliceFromAny(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func aliasOrID(aliases map[string]string, id string) string {
	if a, ok := aliases[id]; ok {
		return a
	}
	return id
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func sumBucketCosts(m map[string]*TokenBucket) float64 {
	var total float64
	for _, b := range m {
		total += b.Cost
	}
	return total
}

// FilterByDate returns entries whose "date" key compares against targetDate
// per op ("==" or ">="). Unknown ops return empty.
func FilterByDate(runs []map[string]any, targetDate, op string) []map[string]any {
	out := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		d, _ := r["date"].(string)
		switch op {
		case "==":
			if d == targetDate {
				out = append(out, r)
			}
		case ">=":
			if d >= targetDate {
				out = append(out, r)
			}
		}
	}
	return out
}

// truncateRunes returns s limited to at most n runes, cutting on a rune
// boundary so the result is always valid UTF-8. A plain byte-slice cut (s[:n])
// can split a multibyte rune and emit U+FFFD into data.json.
func truncateRunes(s string, n int) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// truncateBytes returns s limited to at most maxBytes bytes, backing up to a
// rune boundary so the result is always valid UTF-8. Use this for byte-size
// caps (e.g. a max log-line length); use truncateRunes for visual length caps.
func truncateBytes(s string, maxBytes int) string {
	if maxBytes < 0 {
		maxBytes = 0
	}
	if len(s) <= maxBytes {
		return s
	}
	b := maxBytes
	for b > 0 && !utf8.RuneStart(s[b]) {
		b--
	}
	return s[:b]
}

// LimitSlice truncates s to the first max elements (no-op when shorter).
func LimitSlice[T any](s []T, max int) []T {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func ensureMapMap(m map[string]map[string]float64, key string) map[string]float64 {
	if _, ok := m[key]; !ok {
		m[key] = map[string]float64{}
	}
	return m[key]
}

func ensureMapMapInt(m map[string]map[string]int, key string) map[string]int {
	if _, ok := m[key]; !ok {
		m[key] = map[string]int{}
	}
	return m[key]
}

func sortedJSONKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
