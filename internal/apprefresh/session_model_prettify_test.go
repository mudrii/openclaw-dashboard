package apprefresh

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// TestRunRefreshCollector_SessionModelPrettified closes the one model-display
// panel whose output coupling was untested: the Active Sessions table. A session
// whose raw model id is "zai/glm-5.2" must surface as "GLM-5.2" in the emitted
// data.json — proving collectSessions routes the model through ModelName. If the
// prettify (resolvedModel = ModelName(aliasOrID(...))) were reverted to the raw
// id, this test fails; the unit ModelName/aliasOrID tests alone could not catch
// that wiring regression.
func TestRunRefreshCollector_SessionModelPrettified(t *testing.T) {
	// Gateway down + empty live-session-model + empty catalog, so the session's
	// own raw model field is what gets prettified (deterministically, via the
	// curated GLM fallback rather than a live catalog name).
	prevReadyz := readyzProbe
	readyzProbe = func(context.Context, int) ([]string, bool) { return nil, true }
	t.Cleanup(func() { readyzProbe = prevReadyz })
	stubPgrep(t, "", nil)
	stubHealthz(t, false)

	prevSMC := defaultSessionModelCache
	smc := newLiveSessionModelCache()
	smc.runner = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "") // no live session models
	}
	defaultSessionModelCache = smc
	t.Cleanup(func() { defaultSessionModelCache = prevSMC })

	prevCat := defaultModelCatalogCache.runner
	defaultModelCatalogCache.runner = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "")
	}
	t.Cleanup(func() { defaultModelCatalogCache.runner = prevCat })
	t.Cleanup(resetModelCatalogForTest)

	tmp := t.TempDir()
	dashboardDir := filepath.Join(tmp, "dashboard")
	openclawPath := filepath.Join(tmp, "openclaw")
	for _, d := range []string{
		filepath.Join(openclawPath, "agents", "work", "sessions"),
		filepath.Join(openclawPath, "cron"),
		dashboardDir,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgJSON := `{"agents": {"defaults": {"model": {"primary": "openai/gpt-5"}}, "list": [{"id": "work", "model": "openai/gpt-5"}]}}`
	if err := os.WriteFile(filepath.Join(openclawPath, "openclaw.json"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(openclawPath, "cron", "jobs.json"), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	recentMs := time.Now().Add(-1 * time.Minute).UnixMilli()
	sessions := `{"agent:work:telegram:123:main":{"sessionId":"s1","model":"zai/glm-5.2","updatedAt":` +
		strconv.FormatInt(recentMs, 10) + `}}`
	if err := os.WriteFile(filepath.Join(openclawPath, "agents", "work", "sessions", "sessions.json"), []byte(sessions), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := appconfig.Default()
	cfg.Timezone = "UTC"
	cfg.Refresh.IntervalSeconds = 30
	cfg.AI.GatewayPort = 0

	if err := RunRefreshCollector(context.Background(), dashboardDir, openclawPath, cfg); err != nil {
		t.Fatalf("RunRefreshCollector() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dashboardDir, "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	sess, _ := payload["sessions"].([]any)
	var got string
	for _, s := range sess {
		m, _ := s.(map[string]any)
		if k, _ := m["key"].(string); k == "agent:work:telegram:123:main" {
			got, _ = m["model"].(string)
		}
	}
	if got != "GLM-5.2" {
		t.Errorf("session model = %q, want GLM-5.2 (raw zai/glm-5.2 must be prettified through ModelName)", got)
	}
}
