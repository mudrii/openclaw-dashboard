package main

import (
	"testing"
)

func TestGetMap_Present(t *testing.T) {
	m := map[string]any{"nested": map[string]any{"key": "val"}}
	result := getMap(m, "nested")
	if result["key"] != "val" {
		t.Fatalf("expected val, got %v", result["key"])
	}
}

func TestGetMap_Missing(t *testing.T) {
	m := map[string]any{}
	result := getMap(m, "missing")
	if result == nil {
		t.Fatal("expected empty map, got nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

func TestGetMap_WrongType(t *testing.T) {
	m := map[string]any{"key": "not a map"}
	result := getMap(m, "key")
	if len(result) != 0 {
		t.Fatalf("expected empty map for wrong type, got %v", result)
	}
}

func TestGetStr_Present(t *testing.T) {
	m := map[string]any{"name": "test"}
	if got := getStr(m, "name"); got != "test" {
		t.Fatalf("expected test, got %q", got)
	}
}

func TestGetStr_Missing(t *testing.T) {
	m := map[string]any{}
	if got := getStr(m, "missing"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestGetStr_WrongType(t *testing.T) {
	m := map[string]any{"key": 42}
	if got := getStr(m, "key"); got != "" {
		t.Fatalf("expected empty string for int, got %q", got)
	}
}

func TestGetFloat_Float64(t *testing.T) {
	m := map[string]any{"cost": 3.14}
	if got := getFloat(m, "cost"); got != 3.14 {
		t.Fatalf("expected 3.14, got %f", got)
	}
}

func TestGetFloat_Int(t *testing.T) {
	m := map[string]any{"count": int(42)}
	if got := getFloat(m, "count"); got != 42.0 {
		t.Fatalf("expected 42.0, got %f", got)
	}
}

func TestGetFloat_Missing(t *testing.T) {
	m := map[string]any{}
	if got := getFloat(m, "missing"); got != 0 {
		t.Fatalf("expected 0, got %f", got)
	}
}

func TestGetFloat_WrongType(t *testing.T) {
	m := map[string]any{"key": "not a number"}
	if got := getFloat(m, "key"); got != 0 {
		t.Fatalf("expected 0 for string, got %f", got)
	}
}

func TestGetSlice_Present(t *testing.T) {
	m := map[string]any{"items": []any{"a", "b"}}
	result := getSlice(m, "items")
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
}

func TestGetSlice_Missing(t *testing.T) {
	m := map[string]any{}
	result := getSlice(m, "missing")
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestGetSlice_WrongType(t *testing.T) {
	m := map[string]any{"key": "not a slice"}
	result := getSlice(m, "key")
	if result != nil {
		t.Fatalf("expected nil for wrong type, got %v", result)
	}
}

func TestFmtAny_Nil(t *testing.T) {
	if got := fmtAny(nil); got != "<nil>" {
		t.Fatalf("expected <nil>, got %q", got)
	}
}

func TestFmtAny_String(t *testing.T) {
	if got := fmtAny("hello"); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
}

func TestFmtAny_Float64(t *testing.T) {
	got := fmtAny(float64(3.14))
	if got != "3.14" {
		t.Fatalf("expected 3.14, got %q", got)
	}
}

func TestFmtAny_Int(t *testing.T) {
	if got := fmtAny(int(42)); got != "42" {
		t.Fatalf("expected 42, got %q", got)
	}
}
