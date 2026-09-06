package appopenclaw

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
)

func TestNarrowOperationsCarryExactTargets(t *testing.T) {
	client := Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		var p map[string]any
		if err := json.Unmarshal([]byte(args[len(args)-1]), &p); err != nil {
			t.Fatal(err)
		}
		switch args[2] {
		case "cron.update":
			if p["id"] != "job" || p["expectedConfigRevision"] != "revision" || p["patch"].(map[string]any)["enabled"] != false {
				t.Fatalf("params=%v", p)
			}
		case "cron.run":
			if p["id"] != "job" || p["mode"] != "if-enabled" {
				t.Fatalf("params=%v", p)
			}
		case "chat.abort":
			if p["sessionKey"] != "session" || p["runId"] != "exact-run" {
				t.Fatalf("params=%v", p)
			}
		default:
			t.Fatalf("unexpected method %s", args[2])
		}
		return exec.CommandContext(ctx, "printf", "%s", `{"ok":true}`)
	}}
	var result map[string]any
	if err := client.SetAutomationEnabled(t.Context(), "job", "revision", false, &result); err != nil {
		t.Fatal(err)
	}
	if err := client.RunAutomation(t.Context(), "job", &result); err != nil {
		t.Fatal(err)
	}
	if err := client.AbortRun(t.Context(), "session", "exact-run", &result); err != nil {
		t.Fatal(err)
	}
	if err := client.AbortRun(t.Context(), "session", "", &result); err == nil {
		t.Fatal("refused to require exact run")
	}
	if err := client.SetAutomationEnabled(t.Context(), "job", "", true, &result); err == nil {
		t.Fatal("refused to require revision")
	}
}
