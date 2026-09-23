package apprefresh

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestRuntimeModelReadinessDoesNotExposeAuthDetails(t *testing.T) {
	client := appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		if strings.Join(args, " ") != "models status --agent main --json" {
			t.Fatalf("args=%v", args)
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"allowed":["provider/model"],"resolvedDefault":"provider/model","auth":{"missingProvidersInUse":[],"providers":[{"provider":"provider","effective":{"kind":"missing","detail":"secret-value"},"profiles":{"apiKey":"secret-value"}}]}}`)
	}}
	rows, status := collectRuntimeModels(t.Context(), client, []string{"main"})
	if status.State != "ready" {
		t.Fatalf("status=%+v", status)
	}
	data, _ := json.Marshal(rows)
	if strings.Contains(string(data), "secret-value") || len(rows) != 1 || rows[0]["inferenceVerified"] != false {
		t.Fatalf("rows=%s", data)
	}
	if rows[0]["authKind"] != "missing" {
		t.Fatalf("auth report must not invent readiness: %v", rows)
	}
}

func TestRuntimeModelMissingProviderListIsNotFalse(t *testing.T) {
	client := appopenclaw.Client{Runner: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "%s", `{"allowed":["provider/model"]}`)
	}}
	rows, status := collectRuntimeModels(t.Context(), client, []string{"main"})
	if status.State != "ready" {
		t.Fatalf("status=%+v", status)
	}
	if rows[0]["missingProviderInUse"] != nil {
		t.Fatal("absent provider diagnostics became false")
	}
}

// TestRuntimeModelsKeepAnsweringAgentsWhenOneFails guards against one broken
// agent discarding every other agent's readiness rows.
func TestRuntimeModelsKeepAnsweringAgentsWhenOneFails(t *testing.T) {
	client := appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		if strings.Contains(strings.Join(args, " "), "--agent broken") {
			return exec.CommandContext(ctx, "sh", "-c", "exit 1")
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"allowed":["provider/model"]}`)
	}}
	rows, status := collectRuntimeModels(t.Context(), client, []string{"main", "broken", "work"})
	if len(rows) != 2 || rows[0]["agentId"] != "main" || rows[1]["agentId"] != "work" {
		t.Fatalf("rows=%v", rows)
	}
	if status.State != "partial" || len(status.FailedAgents) != 1 || status.FailedAgents[0] != "broken" {
		t.Fatalf("status=%+v, want partial naming broken", status)
	}

	_, status = collectRuntimeModels(t.Context(), client, []string{"broken"})
	if status.State != "unavailable" {
		t.Fatalf("total failure status=%+v, want unavailable", status)
	}
}
