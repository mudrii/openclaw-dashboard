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
	rows, err := collectRuntimeModels(t.Context(), client, []string{"main"})
	if err != nil {
		t.Fatal(err)
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
	rows, err := collectRuntimeModels(t.Context(), client, []string{"main"})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["missingProviderInUse"] != nil {
		t.Fatal("absent provider diagnostics became false")
	}
}
