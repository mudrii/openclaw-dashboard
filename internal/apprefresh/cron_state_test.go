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

// TestCollectCrons_DeliveryAndFlapping covers INT-5: the dashboard surfaces the
// sidecar delivery status and a flapping indicator derived from consecutiveErrors.
func TestCollectCrons_DeliveryAndFlapping(t *testing.T) {
	build := func(t *testing.T, state map[string]any) map[string]any {
		t.Helper()
		dir := t.TempDir()
		cronPath := filepath.Join(dir, "jobs.json")
		jobs := map[string]any{"jobs": []any{map[string]any{
			"id":       "j1",
			"name":     "n",
			"enabled":  true,
			"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
			"state":    state,
		}}}
		b, _ := json.Marshal(jobs)
		if err := os.WriteFile(cronPath, b, 0o644); err != nil {
			t.Fatal(err)
		}
		crons := CollectCrons(cronPath, time.UTC)
		if len(crons) != 1 {
			t.Fatalf("want 1 cron, got %d", len(crons))
		}
		return crons[0]
	}

	t.Run("delivery status surfaced and flapping set above threshold", func(t *testing.T) {
		c := build(t, map[string]any{
			"lastRunStatus":      "fail",
			"lastDeliveryStatus": "not-delivered",
			"consecutiveErrors":  float64(4),
			"consecutiveSkipped": float64(1),
		})
		if c["lastDeliveryStatus"] != "not-delivered" {
			t.Errorf("lastDeliveryStatus = %v, want not-delivered", c["lastDeliveryStatus"])
		}
		if c["consecutiveErrors"] != 4 {
			t.Errorf("consecutiveErrors = %v, want 4", c["consecutiveErrors"])
		}
		if c["flapping"] != true {
			t.Errorf("flapping = %v, want true (consecutiveErrors >= threshold)", c["flapping"])
		}
	})

	t.Run("healthy job is not flapping", func(t *testing.T) {
		c := build(t, map[string]any{
			"lastRunStatus":      "ok",
			"lastDeliveryStatus": "delivered",
			"consecutiveErrors":  float64(0),
		})
		if c["flapping"] != false {
			t.Errorf("flapping = %v, want false", c["flapping"])
		}
		if c["lastDeliveryStatus"] != "delivered" {
			t.Errorf("lastDeliveryStatus = %v, want delivered", c["lastDeliveryStatus"])
		}
	})

	// Pin the threshold boundary (cronFlappingThreshold == 3) so an off-by-one
	// regression (`> 3` instead of `>= 3`) is caught.
	t.Run("flapping boundary: exactly at threshold is flapping", func(t *testing.T) {
		c := build(t, map[string]any{"consecutiveErrors": float64(3)})
		if c["flapping"] != true {
			t.Errorf("consecutiveErrors=3: flapping = %v, want true (>= threshold)", c["flapping"])
		}
	})
	t.Run("flapping boundary: one below threshold is not flapping", func(t *testing.T) {
		c := build(t, map[string]any{"consecutiveErrors": float64(2)})
		if c["flapping"] != false {
			t.Errorf("consecutiveErrors=2: flapping = %v, want false (below threshold)", c["flapping"])
		}
	})
}

// TestCollectCrons_LastRunStatusPrecedence locks FIX-3: the dashboard reads
// openclaw's canonical lastRunStatus in preference to the deprecated lastStatus
// alias (aligning with openclaw's own readers and future-proofing against alias
// removal), falls back to lastStatus when only the alias is present, and defaults
// to "none" when neither is set.
func TestCollectCrons_LastRunStatusPrecedence(t *testing.T) {
	build := func(t *testing.T, state map[string]any) map[string]any {
		t.Helper()
		dir := t.TempDir()
		cronPath := filepath.Join(dir, "jobs.json")
		jobs := map[string]any{"jobs": []any{map[string]any{
			"id":       "j1",
			"name":     "n",
			"enabled":  true,
			"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
			"state":    state,
		}}}
		b, _ := json.Marshal(jobs)
		if err := os.WriteFile(cronPath, b, 0o644); err != nil {
			t.Fatal(err)
		}
		crons := CollectCrons(cronPath, time.UTC)
		if len(crons) != 1 {
			t.Fatalf("want 1 cron, got %d", len(crons))
		}
		return crons[0]
	}

	t.Run("canonical lastRunStatus wins over deprecated lastStatus", func(t *testing.T) {
		c := build(t, map[string]any{"lastRunStatus": "ok", "lastStatus": "fail"})
		if c["lastStatus"] != "ok" {
			t.Errorf("lastStatus = %v, want ok (canonical lastRunStatus must win)", c["lastStatus"])
		}
	})
	t.Run("falls back to lastStatus when lastRunStatus absent", func(t *testing.T) {
		c := build(t, map[string]any{"lastStatus": "fail"})
		if c["lastStatus"] != "fail" {
			t.Errorf("lastStatus = %v, want fail (legacy fallback)", c["lastStatus"])
		}
	})
	t.Run("neither present yields none", func(t *testing.T) {
		c := build(t, map[string]any{})
		if c["lastStatus"] != "none" {
			t.Errorf("lastStatus = %v, want none (default)", c["lastStatus"])
		}
	})
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

func TestCollectCrons_SidecarPrefersCanonicalLastRunStatus(t *testing.T) {
	dir := t.TempDir()
	cronPath := filepath.Join(dir, "jobs.json")
	statePath := filepath.Join(dir, "jobs-state.json")

	id := "canonical-status-job"
	jobs := map[string]any{
		"jobs": []any{
			map[string]any{
				"id":       id,
				"name":     "canonical",
				"enabled":  true,
				"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
				"state": map[string]any{
					"lastStatus":    "inline-stale",
					"lastRunStatus": "inline-canonical",
				},
			},
		},
	}
	sidecar := map[string]any{
		"jobs": map[string]any{
			id: map[string]any{
				"state": map[string]any{
					"lastStatus":    "sidecar-stale-alias",
					"lastRunStatus": "sidecar-canonical",
				},
			},
		},
	}
	jb, _ := json.Marshal(jobs)
	sb, _ := json.Marshal(sidecar)
	if err := os.WriteFile(cronPath, jb, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, sb, 0o644); err != nil {
		t.Fatal(err)
	}

	crons := CollectCrons(cronPath, time.UTC)
	if got := crons[0]["lastStatus"]; got != "sidecar-canonical" {
		t.Fatalf("lastStatus = %v, want canonical lastRunStatus", got)
	}
}
