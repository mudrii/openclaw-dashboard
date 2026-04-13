package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

func TestResolveSources_AliasAndExactMatch(t *testing.T) {
	configured := []string{
		"logs/gateway.log",
		"logs/gateway.err.log",
		"logs/cron.log",
		"logs/session.log",
		"logs/subagent.log",
	}

	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "gateway alias expands both gateway sources",
			raw:  "gateway",
			want: []string{"logs/gateway.log", "logs/gateway.err.log"},
		},
		{
			name: "cron alias filters cron source",
			raw:  "cron",
			want: []string{"logs/cron.log"},
		},
		{
			name: "session alias filters session source",
			raw:  "session",
			want: []string{"logs/session.log"},
		},
		{
			name: "subagent alias filters subagent source",
			raw:  "subagent",
			want: []string{"logs/subagent.log"},
		},
		{
			name: "exact configured source still works",
			raw:  "logs/session.log",
			want: []string{"logs/session.log"},
		},
		{
			name: "all alias returns all configured sources",
			raw:  "all",
			want: configured,
		},
		{
			name: "unknown source returns empty",
			raw:  "does-not-exist",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSources(tc.raw, configured)
			if !equalStringSlices(got, tc.want) {
				t.Fatalf("resolveSources(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseLogTimestamp(t *testing.T) {
	t.Run("timestamp with tz", func(t *testing.T) {
		line := "2026-04-13T10:15:00Z gateway started"
		ts, seen := parseLogTimestamp(line)
		if ts.IsZero() {
			t.Fatalf("expected valid timestamp, got zero")
		}
		if seen == "" {
			t.Fatalf("expected seen timestamp string, got empty")
		}
	})

	t.Run("timestamp without timezone", func(t *testing.T) {
		line := "2026-04-13 10:15:00 gateway started"
		if _, seen := parseLogTimestamp(line); seen == "" {
			t.Fatalf("expected seen timestamp string, got empty")
		}
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		line := "invalid log line"
		ts, seen := parseLogTimestamp(line)
		if !ts.IsZero() || seen != "" {
			t.Fatalf("expected zero timestamp and empty seen value, got ts=%v seen=%q", ts, seen)
		}
	})
}

func TestParseSince(t *testing.T) {
	t.Run("empty is fallback", func(t *testing.T) {
		if got, err := parseSince("", 123); err != nil || got != 123 {
			t.Fatalf("expected fallback 123, got %d err=%v", got, err)
		}
	})

	t.Run("unix milli", func(t *testing.T) {
		if got, err := parseSince("1713012345678", 0); err != nil || got != 1713012345678 {
			t.Fatalf("expected 1713012345678, got %d err=%v", got, err)
		}
	})

	t.Run("rfc3339", func(t *testing.T) {
		raw := "2026-04-13T10:15:00Z"
		want := time.Date(2026, 4, 13, 10, 15, 0, 0, time.UTC).UnixMilli()
		got, err := parseSince(raw, 0)
		if err != nil || got != want {
			t.Fatalf("expected %d, got %d err=%v", want, got, err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err := parseSince("not-a-time", 9); err == nil {
			t.Fatalf("expected invalid parse error")
		}
	})
}

func TestReadMergedLogs_MergesAndSorts(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	writeLines(t, filepath.Join(dir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z gateway startup",
		"2026-04-13T10:00:02Z gateway request done",
	)
	writeLines(t, filepath.Join(dir, "logs", "cron.log"),
		"2026-04-13T10:00:01Z cron run started",
		"2026-04-13T10:00:03Z cron run done",
	)

	s := &Server{openclawPath: dir}
	records, err := s.readMergedLogs([]string{"logs/gateway.log", "logs/cron.log"}, 3)
	if err != nil {
		t.Fatalf("readMergedLogs failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records after limit, got %d", len(records))
	}

	wantSources := []string{"logs/cron.log", "logs/gateway.log", "logs/cron.log"}
	for i, record := range records {
		if record.Source != wantSources[i] {
			t.Fatalf("record[%d].Source = %q, want %q", i, record.Source, wantSources[i])
		}
	}

	if records[0].TimestampMs >= records[1].TimestampMs {
		t.Fatalf("expected ordered timestamps to be ascending, got %d >= %d", records[0].TimestampMs, records[1].TimestampMs)
	}
	if records[1].TimestampMs >= records[2].TimestampMs {
		t.Fatalf("expected ordered timestamps to be ascending, got %d >= %d", records[1].TimestampMs, records[2].TimestampMs)
	}
}

func TestReadMergedLogs_PrefersNewestEntriesAcrossSkewedSources(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	writeLines(t, filepath.Join(dir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z gateway old",
		"2026-04-13T10:00:01Z gateway still old",
		"2026-04-13T10:00:02Z gateway mid",
		"2026-04-13T10:00:03Z gateway newer",
		"2026-04-13T10:00:04Z gateway newest",
	)
	writeLines(t, filepath.Join(dir, "logs", "cron.log"),
		"2026-04-13T09:59:59Z cron old",
	)

	s := &Server{openclawPath: dir}
	records, err := s.readMergedLogs([]string{"logs/gateway.log", "logs/cron.log"}, 3)
	if err != nil {
		t.Fatalf("readMergedLogs failed: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records after limit, got %d", len(records))
	}

	wantMessages := []string{"gateway mid", "gateway newer", "gateway newest"}
	for i, record := range records {
		if record.Message != wantMessages[i] {
			t.Fatalf("record[%d].Message = %q, want %q", i, record.Message, wantMessages[i])
		}
	}
}

func TestHandleLogs_SourceAlias(t *testing.T) {
	openclawDir := t.TempDir()
	writeLines(t, filepath.Join(openclawDir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z gateway info",
		"2026-04-13T10:00:01Z gateway warn",
	)
	writeLines(t, filepath.Join(openclawDir, "logs", "cron.log"),
		"2026-04-13T10:00:00Z cron info",
		"2026-04-13T10:00:01Z cron warning",
	)

	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Logs.Enabled = true
	cfg.Logs.Sources = []string{"logs/gateway.log", "logs/cron.log"}
	srv := newTestServerWithOpenclawHome(t, cfg, openclawDir)

	req := httptest.NewRequest(http.MethodGet, "/api/logs?source=gateway&limit=20", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var payload struct {
		Entries []struct {
			Source  string `json:"source"`
			Message string `json:"message"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	if len(payload.Entries) == 0 {
		t.Fatal("expected at least one entry")
	}
	for _, entry := range payload.Entries {
		if !strings.Contains(strings.ToLower(entry.Source), "gateway") {
			t.Fatalf("expected gateway-only entries, got source=%q", entry.Source)
		}
		if entry.Message == "" {
			t.Fatal("expected non-empty message")
		}
	}
}

func TestHandleErrors_SourceAlias(t *testing.T) {
	openclawDir := t.TempDir()
	writeLines(t, filepath.Join(openclawDir, "logs", "gateway.log"),
		"2026-04-13T10:00:00Z gateway error: upstream failed",
		"2026-04-13T10:00:01Z gateway info",
	)
	writeLines(t, filepath.Join(openclawDir, "logs", "cron.log"),
		"2026-04-13T10:00:00Z cron warn: job failed",
		"2026-04-13T10:00:01Z cron debug",
	)

	cfg := appconfig.Default()
	cfg.AI.Enabled = false
	cfg.System.Enabled = false
	cfg.Logs.Enabled = true
	cfg.Logs.Sources = []string{"logs/gateway.log", "logs/cron.log"}
	srv := newTestServerWithOpenclawHome(t, cfg, openclawDir)

	req := httptest.NewRequest(http.MethodGet, "/api/errors?source=cron&sort=count&limit=20&windowHours=24", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var payload struct {
		Items []struct {
			Source   string `json:"source"`
			Severity string `json:"severity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}

	for _, item := range payload.Items {
		if !strings.Contains(strings.ToLower(item.Source), "cron") {
			t.Fatalf("expected cron-only error items, got source=%q", item.Source)
		}
		if item.Severity != "warn" && item.Severity != "error" {
			t.Fatalf("expected warn/error severity, got %q", item.Severity)
		}
	}
}

func newTestServerWithOpenclawHome(t *testing.T, cfg appconfig.Config, openclawDir string) *Server {
	t.Helper()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	t.Setenv("OPENCLAW_HOME", openclawDir)

	refreshFn := func(ctx context.Context, d, o string, c ...appconfig.Config) error { return nil }
	return NewServer(dir, "1.0.0", cfg, "", []byte("<html><body>__VERSION__ __RUNTIME__</body></html>"), ctx, refreshFn)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
