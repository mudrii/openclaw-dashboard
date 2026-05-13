package appserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestHandleStaticFile_RejectsSymlinkOutsideRoot ensures a themes.json
// symlink whose target lives outside the served directory is refused.
// Without an EvalSymlinks check, ReadFile would happily follow the link.
func TestHandleStaticFile_RejectsSymlinkOutsideRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}

	rootDir := t.TempDir()
	outsideDir := t.TempDir()

	// Place the real file outside the served root.
	outsidePath := filepath.Join(outsideDir, "themes.json")
	if err := os.WriteFile(outsidePath, []byte(`{"leaked":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside the served root pointing outside.
	linkPath := filepath.Join(rootDir, "themes.json")
	if err := os.Symlink(outsidePath, linkPath); err != nil {
		// Some sandboxes disallow unprivileged symlinks — skip rather than fail.
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	cfg := appconfig.Default()
	cfg.System.Enabled = false
	indexHTML := []byte("<html><head></head><body></body></html>")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	refreshFn := func(ctx context.Context, d, o string, cfg appconfig.Config) error { return nil }
	s := NewServer(rootDir, "1.0.0", cfg, "", indexHTML, ctx, refreshFn)

	req := httptest.NewRequest(http.MethodGet, "/themes.json", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("expected escape-via-symlink to be refused, got 200 with body %q", w.Body.String())
	}
	if w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
		t.Errorf("expected 404 or 403, got %d", w.Code)
	}
}
