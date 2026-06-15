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
