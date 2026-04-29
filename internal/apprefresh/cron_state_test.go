package apprefresh

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// copyTestdata copies testdata/cron/<name> into dir and returns the destination path.
func copyTestdata(t *testing.T, dir, name string) string {
	t.Helper()
	src := filepath.Join("testdata", "cron", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", dst, err)
	}
	return dst
}

// TestCollectCrons_SidecarOnly covers OpenClaw v2026.4.20+ split-store: jobs.json
// has empty inline state and jobs-state.json holds the runtime state.
func TestCollectCrons_SidecarOnly(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	jobsBytes, err := os.ReadFile(filepath.Join("testdata", "cron", "jobs.split-store.json"))
	if err != nil {
		t.Fatal(err)
	}
	stateBytes, err := os.ReadFile(filepath.Join("testdata", "cron", "jobs-state.split-store.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cronPath, jobsBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	crons := CollectCrons(cronPath, time.UTC)
	if len(crons) != 2 {
		t.Fatalf("expected 2 crons, got %d", len(crons))
	}
	first := crons[0]
	if first["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok", first["lastStatus"])
	}
	if first["lastDurationMs"] != 23028 {
		t.Errorf("lastDurationMs = %v, want 23028", first["lastDurationMs"])
	}
	if first["lastRun"] == "" {
		t.Errorf("lastRun should be populated, got empty string")
	}
	if first["nextRun"] == "" {
		t.Errorf("nextRun should be populated, got empty string")
	}
}

// TestCollectCrons_LegacyInlineOnly covers pre-v2026.4.20 single-file behavior
// where state lives inline in jobs.json and there is no sidecar.
func TestCollectCrons_LegacyInlineOnly(t *testing.T) {
	dir := t.TempDir()
	cronPath := copyTestdata(t, dir, "jobs.legacy-inline.json")
	// rename to jobs.json (CollectCrons takes the full path so this is just for clarity)
	finalPath := filepath.Join(dir, "jobs.json")
	if err := os.Rename(cronPath, finalPath); err != nil {
		t.Fatal(err)
	}

	crons := CollectCrons(finalPath, time.UTC)
	if len(crons) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(crons))
	}
	c := crons[0]
	if c["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok", c["lastStatus"])
	}
	if c["lastDurationMs"] != 12345 {
		t.Errorf("lastDurationMs = %v, want 12345", c["lastDurationMs"])
	}
	if c["lastRun"] == "" {
		t.Error("lastRun should be populated from inline state")
	}
	if c["nextRun"] == "" {
		t.Error("nextRun should be populated from inline state")
	}
}

// TestCollectCrons_SidecarMissingJobID: sidecar exists but does not list this job's id.
// Should fall back to inline state for that job.
func TestCollectCrons_SidecarMissingJobID(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	// Job with inline state
	jobs := map[string]any{
		"version": 1,
		"jobs": []any{
			map[string]any{
				"id":       "legacy-uuid-123",
				"name":     "orphan-job",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state": map[string]any{
					"lastStatus":     "ok",
					"lastRunAtMs":    float64(1777025060681),
					"nextRunAtMs":    float64(1777046660670),
					"lastDurationMs": float64(7777),
				},
			},
		},
	}
	jobsBytes, _ := json.Marshal(jobs)
	if err := os.WriteFile(cronPath, jobsBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// Sidecar references a different job id
	sidecar := map[string]any{
		"version": 1,
		"jobs": map[string]any{
			"unrelated-uuid": map[string]any{
				"state": map[string]any{
					"lastStatus":     "fail",
					"lastDurationMs": float64(11111),
				},
			},
		},
	}
	stateBytes, _ := json.Marshal(sidecar)
	if err := os.WriteFile(statePath, stateBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	crons := CollectCrons(cronPath, time.UTC)
	if len(crons) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(crons))
	}
	if crons[0]["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok (from inline)", crons[0]["lastStatus"])
	}
	if crons[0]["lastDurationMs"] != 7777 {
		t.Errorf("lastDurationMs = %v, want 7777 (from inline)", crons[0]["lastDurationMs"])
	}
}

// TestCollectCrons_SidecarMalformed: sidecar JSON is invalid; must not break collection.
// Falls back to inline state.
func TestCollectCrons_SidecarMalformed(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	jobs := map[string]any{
		"jobs": []any{
			map[string]any{
				"id":       "any-id",
				"name":     "x",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state": map[string]any{
					"lastStatus":     "ok",
					"lastRunAtMs":    float64(1777025060681),
					"nextRunAtMs":    float64(1777046660670),
					"lastDurationMs": float64(99),
				},
			},
		},
	}
	jobsBytes, _ := json.Marshal(jobs)
	_ = os.WriteFile(cronPath, jobsBytes, 0o644)
	_ = os.WriteFile(statePath, []byte("not json"), 0o644)

	crons := CollectCrons(cronPath, time.UTC)
	if len(crons) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(crons))
	}
	if crons[0]["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok (inline fallback)", crons[0]["lastStatus"])
	}
	if crons[0]["lastDurationMs"] != 99 {
		t.Errorf("lastDurationMs = %v, want 99 (inline fallback)", crons[0]["lastDurationMs"])
	}
}

// TestCollectCrons_BothPresent_SidecarWins: when inline state and sidecar both have
// values for the same job id, sidecar wins (it is the live runtime state).
func TestCollectCrons_BothPresent_SidecarWins(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	id := "shared-id"
	jobs := map[string]any{
		"jobs": []any{
			map[string]any{
				"id":       id,
				"name":     "x",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state": map[string]any{
					"lastStatus":     "stale",
					"lastDurationMs": float64(1),
				},
			},
		},
	}
	sidecar := map[string]any{
		"jobs": map[string]any{
			id: map[string]any{
				"state": map[string]any{
					"lastStatus":     "ok",
					"lastDurationMs": float64(42),
					"lastRunAtMs":    float64(1777025060681),
					"nextRunAtMs":    float64(1777046660670),
				},
			},
		},
	}
	jb, _ := json.Marshal(jobs)
	sb, _ := json.Marshal(sidecar)
	_ = os.WriteFile(cronPath, jb, 0o644)
	_ = os.WriteFile(statePath, sb, 0o644)

	crons := CollectCrons(cronPath, time.UTC)
	if crons[0]["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok (sidecar wins)", crons[0]["lastStatus"])
	}
	if crons[0]["lastDurationMs"] != 42 {
		t.Errorf("lastDurationMs = %v, want 42 (sidecar wins)", crons[0]["lastDurationMs"])
	}
}

// TestCollectCrons_SidecarEmptyStateFallsBackToInline: when the sidecar has an
// entry for this job id but its `state` field is `{}` (empty object — the OpenClaw
// gateway has registered the job but never populated runtime state, e.g. brand-new
// schedule before its first run), loadCronStateSidecar must skip the entry and
// CollectCrons must fall back to inline state if available. Otherwise the UI would
// show a job with empty lastStatus/lastRun even when a legitimate inline value
// exists (e.g. mid-migration repos where jobs.json still has stale-but-non-empty
// state and jobs-state.json has been initialized with empty objects).
func TestCollectCrons_SidecarEmptyStateFallsBackToInline(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	id := "empty-sidecar-id"
	jobs := map[string]any{
		"jobs": []any{
			map[string]any{
				"id":       id,
				"name":     "x",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state": map[string]any{
					"lastStatus":     "ok",
					"lastRunAtMs":    float64(1777025060681),
					"nextRunAtMs":    float64(1777046660670),
					"lastDurationMs": float64(555),
				},
			},
		},
	}
	sidecar := map[string]any{
		"jobs": map[string]any{
			id: map[string]any{"state": map[string]any{}}, // empty state
		},
	}
	jb, _ := json.Marshal(jobs)
	sb, _ := json.Marshal(sidecar)
	_ = os.WriteFile(cronPath, jb, 0o644)
	_ = os.WriteFile(statePath, sb, 0o644)

	crons := CollectCrons(cronPath, time.UTC)
	if len(crons) != 1 {
		t.Fatalf("expected 1 cron, got %d", len(crons))
	}
	if crons[0]["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok (inline fallback when sidecar state is empty)", crons[0]["lastStatus"])
	}
	if crons[0]["lastDurationMs"] != 555 {
		t.Errorf("lastDurationMs = %v, want 555 (inline fallback)", crons[0]["lastDurationMs"])
	}
}

// TestCollectCrons_SidecarPartialOverridesInlineFully: locks in the documented
// override contract — when sidecar has an entry with non-empty state, it fully
// replaces inline state even when the sidecar entry has fewer populated fields
// than inline. This prevents stale inline data from leaking through into the UI.
// The inline state has lastDurationMs=999; the sidecar has lastStatus only
// (no duration). Result must reflect the sidecar's missing duration as 0,
// not the inline 999.
func TestCollectCrons_SidecarPartialOverridesInlineFully(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	id := "partial-sidecar-id"
	jobs := map[string]any{
		"jobs": []any{
			map[string]any{
				"id":       id,
				"name":     "x",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state": map[string]any{
					"lastStatus":     "stale-from-inline",
					"lastDurationMs": float64(999),
				},
			},
		},
	}
	sidecar := map[string]any{
		"jobs": map[string]any{
			id: map[string]any{
				"state": map[string]any{
					"lastStatus": "ok-from-sidecar",
					// no lastDurationMs; contract says inline 999 must NOT survive
				},
			},
		},
	}
	jb, _ := json.Marshal(jobs)
	sb, _ := json.Marshal(sidecar)
	_ = os.WriteFile(cronPath, jb, 0o644)
	_ = os.WriteFile(statePath, sb, 0o644)

	crons := CollectCrons(cronPath, time.UTC)
	if crons[0]["lastStatus"] != "ok-from-sidecar" {
		t.Errorf("lastStatus = %v, want ok-from-sidecar", crons[0]["lastStatus"])
	}
	if crons[0]["lastDurationMs"] != 0 {
		t.Errorf("lastDurationMs = %v, want 0 — sidecar must fully replace inline, not merge",
			crons[0]["lastDurationMs"])
	}
}

// TestCollectCrons_SidecarLastRunStatusFallback: when sidecar omits lastStatus but
// has lastRunStatus, use lastRunStatus.
func TestCollectCrons_SidecarLastRunStatusFallback(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	id := "6f34de99-6a2d-4b20-89e4-65a5e4410c1b"
	jobs := map[string]any{
		"jobs": []any{
			map[string]any{
				"id":       id,
				"name":     "x",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state":    map[string]any{},
			},
		},
	}
	jb, _ := json.Marshal(jobs)
	_ = os.WriteFile(cronPath, jb, 0o644)
	stateBytes, err := os.ReadFile(filepath.Join("testdata", "cron", "jobs-state.lastRunStatusOnly.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(statePath, stateBytes, 0o644)

	crons := CollectCrons(cronPath, time.UTC)
	if crons[0]["lastStatus"] != "fail" {
		t.Errorf("lastStatus = %v, want fail (from lastRunStatus)", crons[0]["lastStatus"])
	}
	if crons[0]["lastDurationMs"] != 99 {
		t.Errorf("lastDurationMs = %v, want 99", crons[0]["lastDurationMs"])
	}
}
