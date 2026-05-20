package appsystem

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestInt64FromAny exercises int64FromAny across supported and unsupported input types.
func TestInt64FromAny(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		wantVal int64
		wantOK  bool
	}{
		{"int", int(42), 42, true},
		{"int64", int64(9000), 9000, true},
		{"float64", float64(3.7), 3, true},
		{"json.Number valid", json.Number("1234"), 1234, true},
		{"json.Number invalid", json.Number("abc"), 0, false},
		{"nil", nil, 0, false},
		{"unknown type string", "abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := int64FromAny(tc.in)
			if got != tc.wantVal {
				t.Errorf("value: want %d, got %d", tc.wantVal, got)
			}
			if ok != tc.wantOK {
				t.Errorf("ok: want %v, got %v", tc.wantOK, ok)
			}
		})
	}
}

// TestStringSliceFromAny exercises stringSliceFromAny including filtering of empties and non-strings.
func TestStringSliceFromAny(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want []string
	}{
		{"nil", nil, nil},
		{"non-[]any input", "not a slice", nil},
		{"empty []any", []any{}, []string{}},
		{"mixed strings and non-strings", []any{"a", 1, "b", true, "c"}, []string{"a", "b", "c"}},
		{"empty strings dropped", []any{"", "x", "", "y"}, []string{"x", "y"}},
		{"all strings happy path", []any{"alpha", "beta", "gamma"}, []string{"alpha", "beta", "gamma"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stringSliceFromAny(tc.in)
			if tc.want == nil {
				if got != nil {
					t.Errorf("want nil slice, got %#v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Errorf("length: want %d, got %d (%#v)", len(tc.want), len(got), got)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: want %q, got %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

// TestParseOpenclawStatusJSON covers happy-path parsing, fallback to versions, key precedence, malformed JSON, and log-prefixed output.
func TestParseOpenclawStatusJSON(t *testing.T) {
	t.Run("clean valid JSON with all fields", func(t *testing.T) {
		out := `{"currentVersion":"1.2.3","latestVersion":"1.2.4","connectLatencyMs":42,"security":{"signed":true}}`
		got, err := parseOpenclawStatusJSON(out, SystemVersions{Openclaw: "ignored", Latest: "ignored2"})
		if err != nil {
			t.Errorf("err: want nil, got %v", err)
		}
		if got.CurrentVersion != "1.2.3" {
			t.Errorf("CurrentVersion: want %q, got %q", "1.2.3", got.CurrentVersion)
		}
		if got.LatestVersion != "1.2.4" {
			t.Errorf("LatestVersion: want %q, got %q", "1.2.4", got.LatestVersion)
		}
		if got.ConnectLatencyMs != 42 {
			t.Errorf("ConnectLatencyMs: want %d, got %d", 42, got.ConnectLatencyMs)
		}
		if got.Security == nil {
			t.Errorf("Security: want non-nil map, got nil")
		} else if v, ok := got.Security["signed"].(bool); !ok || !v {
			t.Errorf("Security[signed]: want true, got %v", got.Security["signed"])
		}
	})

	t.Run("missing currentVersion falls back to versions.Openclaw", func(t *testing.T) {
		out := `{"latestVersion":"9.9.9"}`
		got, err := parseOpenclawStatusJSON(out, SystemVersions{Openclaw: "fallback-cur", Latest: "fallback-lat"})
		if err != nil {
			t.Errorf("err: want nil, got %v", err)
		}
		if got.CurrentVersion != "fallback-cur" {
			t.Errorf("CurrentVersion: want %q, got %q", "fallback-cur", got.CurrentVersion)
		}
		if got.LatestVersion != "9.9.9" {
			t.Errorf("LatestVersion: want %q, got %q", "9.9.9", got.LatestVersion)
		}
	})

	t.Run("version key used when currentVersion empty and versions.Openclaw empty", func(t *testing.T) {
		out := `{"version":"7.7.7"}`
		got, err := parseOpenclawStatusJSON(out, SystemVersions{Openclaw: "", Latest: ""})
		if err != nil {
			t.Errorf("err: want nil, got %v", err)
		}
		if got.CurrentVersion != "7.7.7" {
			t.Errorf("CurrentVersion: want %q, got %q", "7.7.7", got.CurrentVersion)
		}
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		out := `{"currentVersion":`
		_, err := parseOpenclawStatusJSON(out, SystemVersions{})
		if err == nil {
			t.Errorf("err: want non-nil, got nil")
		}
	})

	t.Run("output with log preamble containing brace recovers JSON object", func(t *testing.T) {
		out := `[INFO] starting up {ignored} {"currentVersion":"5.0.0"}`
		got, err := parseOpenclawStatusJSON(out, SystemVersions{})
		if err != nil {
			t.Errorf("err: want nil (scan should advance past '{ignored}'), got %v", err)
		}
		if got.CurrentVersion != "5.0.0" {
			t.Errorf("CurrentVersion: want %q, got %q", "5.0.0", got.CurrentVersion)
		}
	})
}

// TestDecodeJSONObjectFromOutput documents decodeJSONObjectFromOutput behavior including the first-brace-wins limitation.
func TestDecodeJSONObjectFromOutput(t *testing.T) {
	t.Run("clean JSON", func(t *testing.T) {
		var m map[string]any
		if err := decodeJSONObjectFromOutput(`{"a":1}`, &m); err != nil {
			t.Errorf("err: want nil, got %v", err)
		}
		if v, ok := m["a"].(float64); !ok || v != 1 {
			t.Errorf("a: want 1, got %v", m["a"])
		}
	})

	t.Run("no brace returns error", func(t *testing.T) {
		var m map[string]any
		err := decodeJSONObjectFromOutput("no json here", &m)
		if err == nil {
			t.Errorf("err: want non-nil, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "json object not found") {
			t.Errorf("err text: want contains %q, got %q", "json object not found", err.Error())
		}
	})

	t.Run("valid JSON object plain", func(t *testing.T) {
		var m map[string]any
		if err := decodeJSONObjectFromOutput(`{"k":"v"}`, &m); err != nil {
			t.Errorf("err: want nil, got %v", err)
		}
		if m["k"] != "v" {
			t.Errorf("k: want %q, got %v", "v", m["k"])
		}
	})

	t.Run("log line containing brace before JSON object — scan past invalid object", func(t *testing.T) {
		var m map[string]any
		if err := decodeJSONObjectFromOutput(`[INFO] starting up {wrong} {"k":"v"}`, &m); err != nil {
			t.Errorf("err: want nil (scan should advance past invalid '{wrong}'), got %v", err)
		}
		if m["k"] != "v" {
			t.Errorf("k: want %q, got %v", "v", m["k"])
		}
	})

	t.Run("plain leading text without brace before JSON object succeeds", func(t *testing.T) {
		var m map[string]any
		if err := decodeJSONObjectFromOutput(`some log line no braces here then `+`{"k":"v"}`, &m); err != nil {
			t.Errorf("err: want nil, got %v", err)
		}
		if m["k"] != "v" {
			t.Errorf("k: want %q, got %v", "v", m["k"])
		}
	})
}
