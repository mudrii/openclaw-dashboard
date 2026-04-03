package main

import (
	"fmt"
	"strconv"
)

// getMap safely extracts a map[string]any from a parent map.
// Returns an empty map if the key is missing or the wrong type.
func getMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

// getStr safely extracts a string from a map.
// Returns "" if the key is missing or the wrong type.
func getStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// getFloat safely extracts a float64 from a map.
// Handles both float64 (standard JSON number) and int types.
func getFloat(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// getSlice safely extracts a []any from a map.
// Returns nil if the key is missing or the wrong type.
func getSlice(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

// fmtAny formats any value to string for prompt building.
func fmtAny(v any) string {
	if v == nil {
		return "<nil>"
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return fmt.Sprint(v)
	}
}
