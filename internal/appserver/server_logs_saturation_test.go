package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestHandleErrors_ReportsDroppedSignaturesWhenSaturated ensures /api/errors
// surfaces a count of signatures silently dropped after the dedup map fills.
// Without the counter the saturation guard is invisible to operators.
func TestHandleErrors_ReportsDroppedSignaturesWhenSaturated(t *testing.T) {
	openclawDir := t.TempDir()
	logsDir := filepath.Join(openclawDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Generate distinct error messages so each yields a unique signature.
	// Use recent timestamps so the default 24h window keeps all entries.
	const total = 10
	now := time.Now().UTC()
	lines := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ts := now.Add(-time.Duration(total-i) * time.Minute).Format("2006-01-02T15:04:05Z")
		// Use a non-numeric distinct token so NormalizeErrorSignature does
		// not collapse them into a single signature.
		tag := string(rune('a' + i))
		lines = append(lines, fmt.Sprintf("%s error gateway distinct token alpha%s end", ts, tag))
	}
	writeLines(t, filepath.Join(logsDir, "gateway.log"), lines...)

	cfg := appconfig.Default()
	cfg.System.Enabled = false
	cfg.AI.Enabled = false
	cfg.Logs.Enabled = true
	cfg.Logs.Sources = []string{"logs/gateway.log"}
	cfg.Logs.MaxErrorSignatures = 3 // far below `total` to force saturation

	indexHTML := []byte("<html><head></head><body></body></html>")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refreshFn := func(ctx context.Context, d, o string, cfg appconfig.Config) error { return nil }
	s := NewServer(t.TempDir(), "1.0.0", cfg, "", indexHTML, ctx, refreshFn)
	s.openclawPath = openclawDir // point log reader at the test fixture

	req := httptest.NewRequest(http.MethodGet, "/api/errors", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}

	dropped, ok := resp["dropped_signatures"]
	if !ok {
		t.Fatalf("missing dropped_signatures field; body=%s", w.Body.String())
	}
	d, ok := dropped.(float64)
	if !ok {
		t.Fatalf("dropped_signatures not a number: %T", dropped)
	}
	want := float64(total - cfg.Logs.MaxErrorSignatures)
	if d != want {
		t.Fatalf("dropped_signatures = %v, want %v (body=%s)", d, want, w.Body.String())
	}
}
