package apprefresh

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appconfig"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func rpcFixtureClient(t *testing.T, response func(string, map[string]any) string) appopenclaw.Client {
	t.Helper()
	return appopenclaw.Client{Binary: "openclaw", Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		var params map[string]any
		if err := json.Unmarshal([]byte(args[len(args)-1]), &params); err != nil {
			t.Fatal(err)
		}
		return exec.CommandContext(ctx, "printf", "%s", response(args[2], params))
	}}
}

func TestUsesRuntimeDataForInheritedContainerWithoutLocalFiles(t *testing.T) {
	t.Setenv("OPENCLAW_CONTAINER", "fixture-container")
	if !UsesRuntimeData(t.TempDir(), appopenclaw.Target{}) {
		t.Fatal("inherited container selection must not use legacy host-file collectors")
	}
}

func TestDashboardRuntimeCostAlerts(t *testing.T) {
	for _, tc := range []struct {
		name, cache string
		missing     int
		wantAlert   bool
	}{
		{"complete expensive day", "fresh", 0, true},
		{"missing prices", "fresh", 1, false},
		{"warming", "refreshing", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := execCommandContext
			t.Cleanup(func() { execCommandContext = previous; resetModelCatalogForTest() })
			execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				body := `{}`
				if len(args) > 2 && args[0] == "gateway" {
					switch args[2] {
					case "sessions.list":
						body = `{"sessions":[],"hasMore":false}`
					case "tasks.list":
						body = `{"tasks":[]}`
					case "sessions.usage":
						body = fmt.Sprintf(`{"totals":{"totalCost":75,"missingCostEntries":%d},"cacheStatus":{"status":%q}}`, tc.missing, tc.cache)
					}
				}
				return exec.CommandContext(ctx, "printf", "%s", body)
			}
			cfg := appconfig.Default()
			cfg.Openclaw = appopenclaw.Target{Mode: "container", Container: "fixture"}
			data := collectDashboardData(t.Context(), t.TempDir(), t.TempDir(), cfg)
			found := false
			for _, alert := range data["alerts"].([]map[string]any) {
				found = found || strings.Contains(jsonStr(alert, "message"), "High daily cost: $75.00")
			}
			if found != tc.wantAlert {
				t.Fatalf("cost alert=%v want=%v alerts=%v", found, tc.wantAlert, data["alerts"])
			}
		})
	}
}

func TestRuntimeSessionsNoLegacyFiles(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if method != "sessions.list" {
			t.Fatalf("method=%s", method)
		}
		if params["offset"] == float64(0) {
			return `{"sessions":[{"key":"agent:main:main","sessionId":"s1","agentId":"main","model":"m","modelProvider":"p","displayName":"Current","totalTokensFresh":false,"totalTokens":12,"contextTokens":100,"hasActiveRun":true,"activeRunIds":["r1"],"placement":{"kind":"local"},"goal":{"objective":"finish"}}],"hasMore":true,"nextOffset":1,"totalCount":2}`
		}
		return `{"sessions":[{"key":"agent:main:other","sessionId":"s2","agentId":"main","totalTokensFresh":true,"totalTokens":50,"contextTokens":100,"hasActiveRun":false,"activeRunIds":[]}],"hasMore":false,"totalCount":2}`
	})
	snapshot, err := collectRuntimeSessions(t.Context(), client, time.UTC, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Rows) != 2 || snapshot.Total != 2 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	first := snapshot.Rows[0]
	if first["model"] != "p/m" || first["active"] != true || first["contextPct"] != nil || first["totalTokens"] != nil {
		t.Fatalf("first=%v", first)
	}
	if first["goal"] == nil || first["placement"] == nil {
		t.Fatalf("lost runtime state: %v", first)
	}
	if snapshot.Rows[1]["contextPct"] != float64(50) {
		t.Fatalf("second=%v", snapshot.Rows[1])
	}
}

func TestRuntimeSessionsRejectMalformedAndNonadvancingPages(t *testing.T) {
	for _, body := range []string{`{}`, `{"sessions":null}`, `{"sessions":[],"hasMore":true,"nextOffset":0}`} {
		client := rpcFixtureClient(t, func(string, map[string]any) string { return body })
		if _, err := collectRuntimeSessions(t.Context(), client, time.UTC, nil); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	client := rpcFixtureClient(t, func(string, map[string]any) string { return `{"sessions":[],"hasMore":false,"totalCount":0}` })
	snapshot, err := collectRuntimeSessions(t.Context(), client, time.UTC, nil)
	if err != nil || snapshot.Rows == nil || len(snapshot.Rows) != 0 {
		t.Fatalf("empty=%+v %v", snapshot, err)
	}
}

func TestRuntimeTasksKeepIdentityAndDelivery(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if method != "tasks.list" {
			t.Fatalf("method=%s", method)
		}
		if params["cursor"] == nil {
			return `{"tasks":[{"id":"one","title":"same","runtime":"subagent","status":"completed","createdAt":1000,"endedAt":2000,"deliveryStatus":"not_applicable"}],"nextCursor":"page2"}`
		}
		return `{"tasks":[{"id":"two","title":"same","runtime":"cron","status":"completed","terminalOutcome":"failed","createdAt":1000,"endedAt":2000}],"nextCursor":null}`
	})
	rows, err := collectRuntimeTasks(t.Context(), client, time.UTC)
	if err != nil || len(rows) != 2 {
		t.Fatalf("tasks=%v %v", rows, err)
	}
	if rows[0]["id"] != "one" || rows[1]["id"] != "two" || rows[0]["status"] != "succeeded" || rows[1]["status"] != "failed" {
		t.Fatalf("tasks=%v", rows)
	}
	if rows[0]["deliveryStatus"] != "not_applicable" {
		t.Fatal("delivery lost")
	}
}

func TestDashboardUsesRuntimeSessionsWithoutLegacyFiles(t *testing.T) {
	previous := execCommandContext
	t.Cleanup(func() { execCommandContext = previous; resetModelCatalogForTest() })
	execCommandContext = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		body := `{}`
		if len(args) > 2 && args[0] == "gateway" && args[2] == "sessions.list" {
			body = `{"sessions":[{"key":"agent:main:main","sessionId":"s","agentId":"main","displayName":"Live session","hasActiveRun":true}],"hasMore":false,"totalCount":1}`
		}
		if len(args) > 2 && args[0] == "gateway" && args[2] == "tasks.list" {
			body = `{"tasks":[]}`
		}
		return exec.CommandContext(ctx, "printf", "%s", body)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "agents", "main", "agent"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "main", "agent", "openclaw-agent.sqlite"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Default()
	cfg.AI.GatewayPort = 0
	data := collectDashboardData(t.Context(), t.TempDir(), root, cfg)
	rows, _ := data["sessions"].([]map[string]any)
	if len(rows) != 1 || rows[0]["name"] != "Live session" || data["sessionCount"] != 1 {
		t.Fatalf("sessions=%v count=%v", rows, data["sessionCount"])
	}
	collections, _ := data["collections"].(map[string]CollectionStatus)
	if collections["sessions"].State != "ready" {
		t.Fatalf("collections=%v", collections)
	}
}
