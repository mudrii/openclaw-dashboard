package apprefresh

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestCollectSubagentRuns_StubRunner proves subagent runs are sourced from
// `openclaw tasks list --json --runtime subagent` (OpenClaw moved subagent
// tracking out of session keys into the durable tasks store). Each run carries
// agent, task, status, duration, timestamp, and error — but NO cost/token data,
// which the tasks API does not expose post-migration.
func TestCollectSubagentRuns_StubRunner(t *testing.T) {
	fixture := filepath.Join("testdata", "cron", "subagent-tasks.cli.json")
	runs := collectSubagentRuns(context.Background(), catRunner(fixture), func() string { return "openclaw" }, time.UTC)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	r := runs[0]
	if r["agent"] != "main" {
		t.Errorf("agent = %v, want main", r["agent"])
	}
	if r["status"] != "succeeded" {
		t.Errorf("status = %v, want succeeded", r["status"])
	}
	if r["durationSec"] != 207 {
		t.Errorf("durationSec = %v, want 207 (ended-started)", r["durationSec"])
	}
	if r["task"] == "" {
		t.Errorf("task should be populated")
	}
	if r["timestamp"] == "" {
		t.Errorf("timestamp should be populated from createdAt")
	}
	if r["date"] == "" {
		t.Errorf("date should be populated for windowing (FilterByDate)")
	}
	// cost/tokens are intentionally absent — no source post-migration.
	if _, ok := r["cost"]; ok {
		t.Errorf("cost key must be absent (no source); got %v", r["cost"])
	}
}

// TestCollectSubagentRuns_RunnerErrorEmpty: a failing CLI yields an empty list
// (the legacy session-key source is gone, so there is no file fallback — an
// empty Sub-Agent panel is the honest result).
func TestCollectSubagentRuns_RunnerErrorEmpty(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}
	runs := collectSubagentRuns(context.Background(), runner, func() string { return "openclaw" }, time.UTC)
	if len(runs) != 0 {
		t.Errorf("got %d runs, want 0 on runner error", len(runs))
	}
}

// TestCollectSubagentRuns_DurationFallbackToCreated: when startedAt is absent,
// duration falls back to endedAt-createdAt so a run still shows a duration.
func TestCollectSubagentRuns_DurationFallbackToCreated(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", `%s`,
			`{"tasks":[{"agentId":"work","task":"t","status":"failed","createdAt":1781494400000,"endedAt":1781494460000,"error":"boom"}]}`)
	}
	runs := collectSubagentRuns(context.Background(), runner, func() string { return "openclaw" }, time.UTC)
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0]["durationSec"] != 60 {
		t.Errorf("durationSec = %v, want 60 (ended-created fallback)", runs[0]["durationSec"])
	}
	if runs[0]["status"] != "failed" {
		t.Errorf("status = %v, want failed", runs[0]["status"])
	}
	if runs[0]["error"] != "boom" {
		t.Errorf("error = %v, want boom", runs[0]["error"])
	}
}

// TestSubagentTasksFromBytes covers the envelope form, the bare-array form, and
// graceful failure on invalid input.
func TestSubagentTasksFromBytes(t *testing.T) {
	t.Run("envelope", func(t *testing.T) {
		ts, ok := subagentTasksFromBytes([]byte(`{"tasks":[{"taskId":"a"}]}`))
		if !ok || len(ts) != 1 {
			t.Fatalf("ok=%v len=%d, want true/1", ok, len(ts))
		}
	})
	t.Run("bare array", func(t *testing.T) {
		ts, ok := subagentTasksFromBytes([]byte(`[{"taskId":"a"},{"taskId":"b"}]`))
		if !ok || len(ts) != 2 {
			t.Fatalf("ok=%v len=%d, want true/2", ok, len(ts))
		}
	})
	t.Run("invalid", func(t *testing.T) {
		if _, ok := subagentTasksFromBytes([]byte(`nope`)); ok {
			t.Error("ok=true, want false on invalid json")
		}
	})
}

// TestCollectSubagentRuns_NegativeDurationClampedToZero guards the duration
// guard: a task whose endedAt precedes its start (clock skew / bad record) must
// yield durationSec 0, never a negative number.
func TestCollectSubagentRuns_NegativeDurationClampedToZero(t *testing.T) {
	runner := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", `%s`,
			`{"tasks":[{"agentId":"main","task":"t","status":"failed","createdAt":1781494500000,"startedAt":1781494500000,"endedAt":1781494400000}]}`)
	}
	runs := collectSubagentRuns(context.Background(), runner, func() string { return "openclaw" }, time.UTC)
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	if runs[0]["durationSec"] != 0 {
		t.Errorf("durationSec = %v, want 0 (ended < started must not go negative)", runs[0]["durationSec"])
	}
}
