package apprefresh

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// catRunner returns a runner stub that emits the contents of a fixture file,
// ignoring the requested command/args (the unit under test only cares about the
// stdout JSON).
func catRunner(path string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "cat", path)
	}
}

// TestCollectCronsViaCLI_StubRunner proves crons are sourced from
// `openclaw cron list --json` (OpenClaw's post-SQLite-migration store), whose
// per-job inline `state` carries the same fields the legacy jobs-state.json
// sidecar did — so schedule, status, duration, delivery, and flapping all map
// through without a file on disk.
func TestCollectCronsViaCLI_StubRunner(t *testing.T) {
	fixture := filepath.Join("testdata", "cron", "cron-list.cli.json")
	crons, ok := collectCronsViaCLI(context.Background(), catRunner(fixture), func() string { return "openclaw" }, time.UTC)
	if !ok {
		t.Fatal("collectCronsViaCLI ok = false, want true (CLI succeeded)")
	}
	if len(crons) != 2 {
		t.Fatalf("got %d crons, want 2", len(crons))
	}
	first := crons[0]
	if first["name"] != "Vault Inbox Pipeline" {
		t.Errorf("name = %v, want Vault Inbox Pipeline", first["name"])
	}
	if first["lastStatus"] != "ok" {
		t.Errorf("lastStatus = %v, want ok (from inline CLI state)", first["lastStatus"])
	}
	if first["lastDurationMs"] != 30929 {
		t.Errorf("lastDurationMs = %v, want 30929", first["lastDurationMs"])
	}
	if first["schedule"] == "" {
		t.Errorf("schedule should be populated, got empty")
	}
	if first["nextRun"] == "" {
		t.Errorf("nextRun should be populated from inline state")
	}
	if first["flapping"] != false {
		t.Errorf("flapping = %v, want false (consecutiveErrors 0)", first["flapping"])
	}
	if _, ok := first["lastDeliveryStatus"]; !ok {
		t.Errorf("lastDeliveryStatus key missing")
	}
}

// TestCollectCronsViaCLI_RunnerError signals fallback: a failing CLI (gateway
// down, binary missing) returns ok=false so the caller can fall back to the
// legacy jobs.json file.
func TestCollectCronsViaCLI_RunnerError(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false") // exits non-zero, no output
	}
	crons, ok := collectCronsViaCLI(context.Background(), runner, func() string { return "openclaw" }, time.UTC)
	if ok {
		t.Errorf("ok = true, want false on runner error")
	}
	if crons != nil {
		t.Errorf("crons = %v, want nil on runner error", crons)
	}
}

// TestCollectCronsViaCLI_EmptyJobsAuthoritative: a valid empty response
// (`{"jobs":[]}`) is authoritative — ok=true, no fallback — because a host with
// zero cron jobs is a legitimate state, not a CLI failure.
func TestCollectCronsViaCLI_EmptyJobsAuthoritative(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", `%s`, `{"jobs":[]}`)
	}
	crons, ok := collectCronsViaCLI(context.Background(), runner, func() string { return "openclaw" }, time.UTC)
	if !ok {
		t.Errorf("ok = false, want true (valid empty response is authoritative)")
	}
	if len(crons) != 0 {
		t.Errorf("got %d crons, want 0", len(crons))
	}
}

// TestCollectCronsViaCLI_FlappingFromInlineState proves the INT-5 flapping signal
// is derived from the CLI job's inline state just as it was from the sidecar.
func TestCollectCronsViaCLI_FlappingFromInlineState(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", `%s`,
			`{"jobs":[{"id":"j1","name":"n","enabled":true,`+
				`"schedule":{"kind":"cron","expr":"0 0 * * *"},`+
				`"state":{"lastRunStatus":"fail","consecutiveErrors":4,"lastDeliveryStatus":"not-delivered","lastDiagnostics":["x","y"]}}]}`)
	}
	crons, ok := collectCronsViaCLI(context.Background(), runner, func() string { return "openclaw" }, time.UTC)
	if !ok || len(crons) != 1 {
		t.Fatalf("ok=%v len=%d, want true/1", ok, len(crons))
	}
	c := crons[0]
	if c["flapping"] != true {
		t.Errorf("flapping = %v, want true (consecutiveErrors 4 >= threshold)", c["flapping"])
	}
	if c["lastStatus"] != "fail" {
		t.Errorf("lastStatus = %v, want fail (canonical lastRunStatus)", c["lastStatus"])
	}
	diag, _ := c["lastDiagnostics"].([]string)
	if !slices.Equal(diag, []string{"x", "y"}) {
		t.Errorf("lastDiagnostics = %v, want [x y]", c["lastDiagnostics"])
	}
}

// TestCronJobToMap_ModelPrettified locks the cron MODEL column to the same
// prettified display the rest of the dashboard uses: the payload's raw model id
// is rendered through ModelName (catalog/curated), not shown raw.
func TestCronJobToMap_ModelPrettified(t *testing.T) {
	prev := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prev) })
	setModelCatalogNames(map[string]string{"minimax/MiniMax-M3": "MiniMax M3"})

	jm := map[string]any{
		"id": "j", "name": "n", "enabled": true,
		"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"},
		"payload":  map[string]any{"kind": "agentTurn", "model": "minimax/MiniMax-M3"},
	}
	c := cronJobToMap(jm, nil, time.UTC)
	if c["model"] != "MiniMax M3" {
		t.Errorf("model = %v, want MiniMax M3 (prettified)", c["model"])
	}
	// empty payload model stays empty (frontend renders "—").
	jm2 := map[string]any{"id": "j2", "name": "n2", "enabled": true,
		"schedule": map[string]any{"kind": "cron", "expr": "0 0 * * *"}}
	if c2 := cronJobToMap(jm2, nil, time.UTC); c2["model"] != "" {
		t.Errorf("missing model = %v, want empty", c2["model"])
	}
}

// TestCronScheduleString covers every schedule kind the renderer must format:
// cron expr, the "every" duration tiers (d/h/m/ms), the truncated "at" form, and
// the raw-JSON default for an unknown kind.
func TestCronScheduleString(t *testing.T) {
	tests := []struct {
		name  string
		sched map[string]any
		want  string
	}{
		{"cron expr", map[string]any{"kind": "cron", "expr": "0 0 * * *"}, "0 0 * * *"},
		{"every days", map[string]any{"kind": "every", "everyMs": float64(2 * 86400000)}, "Every 2d"},
		{"every hours", map[string]any{"kind": "every", "everyMs": float64(3600000)}, "Every 1h"},
		{"every minutes", map[string]any{"kind": "every", "everyMs": float64(300000)}, "Every 5m"},
		{"every millis", map[string]any{"kind": "every", "everyMs": float64(500)}, "Every 500ms"},
		{"at truncated to 16", map[string]any{"kind": "at", "at": "2026-06-15T10:00:00Z"}, "2026-06-15T10:00"},
		{"unknown kind falls back to raw json", map[string]any{"kind": "weird"}, `{"kind":"weird"}`},
		{"nil schedule", nil, "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cronScheduleString(tt.sched); got != tt.want {
				t.Errorf("cronScheduleString(%v) = %q, want %q", tt.sched, got, tt.want)
			}
		})
	}
}

// TestCronJobsFromBytes covers the envelope form, the bare-array form (which the
// gateway CLI can emit), and graceful failure on invalid input.
func TestCronJobsFromBytes(t *testing.T) {
	t.Run("envelope", func(t *testing.T) {
		jobs, ok := cronJobsFromBytes([]byte(`{"jobs":[{"id":"a"}]}`))
		if !ok || len(jobs) != 1 {
			t.Fatalf("ok=%v len=%d, want true/1", ok, len(jobs))
		}
	})
	t.Run("bare array", func(t *testing.T) {
		jobs, ok := cronJobsFromBytes([]byte(`[{"id":"a"},{"id":"b"}]`))
		if !ok || len(jobs) != 2 {
			t.Fatalf("ok=%v len=%d, want true/2", ok, len(jobs))
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		if _, ok := cronJobsFromBytes([]byte(`{not json`)); ok {
			t.Error("ok=true, want false on invalid json")
		}
	})
	t.Run("object without jobs key", func(t *testing.T) {
		if _, ok := cronJobsFromBytes([]byte(`{"count":0}`)); ok {
			t.Error("ok=true, want false when neither envelope.jobs nor a bare array")
		}
	})
}
