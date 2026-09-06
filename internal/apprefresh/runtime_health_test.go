package apprefresh

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestRuntimeHealthProjectionDoesNotLeakCredentialsOrMemory(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, _ map[string]any) string {
		switch method {
		case "doctor.memory.status":
			return `{"agentId":"main","provider":"ollama","embedding":{"ok":false,"checked":false,"error":"not checked secret-value"},"dreaming":{"enabled":true,"shortTermCount":1,"shortTermEntries":[{"snippet":"private-memory"}],"phases":{"light":{"enabled":true,"nextRunAtMs":100}}}}`
		case "skills.status":
			return `{"agentId":"main","skills":[{"name":"test","eligible":false,"disabled":false,"missing":{"env":["KEY_NAME"]},"apiKey":"secret-value"}]}`
		case "channels.status":
			return `{"channelAccounts":{"telegram":[{"accountId":"good","connected":true,"running":true},{"accountId":"bad","connected":false,"lastError":"secret-value"},{"accountId":"unknown","enabled":true,"configured":true}]}}`
		case "status":
			return `{"runtimeVersion":"2026.8.2","degradedPlugins":[],"degradedSecretOwners":[],"processMemory":{"rss":100}}`
		default:
			return `{}`
		}
	})
	data, statuses := collectRuntimeHealth(t.Context(), client, []string{"main"})
	encoded, _ := json.Marshal(data)
	if strings.Contains(string(encoded), "secret-value") || strings.Contains(string(encoded), "private-memory") {
		t.Fatalf("sensitive projection: %s", encoded)
	}
	if statuses["channels"].State != "ready" || statuses["memory"].State != "ready" {
		t.Fatalf("statuses=%v", statuses)
	}
	channels := data["channels"].([]map[string]any)
	if len(channels) != 3 || channels[0]["connected"] != true || channels[1]["connected"] != false || channels[2]["connected"] != nil {
		t.Fatalf("channels=%v", channels)
	}
	memory := data["memory"].([]map[string]any)
	if asObj(memory[0]["embedding"])["checked"] != false {
		t.Fatalf("memory=%v", memory)
	}
}

func TestRuntimeProcessInfoProjection(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ready      bool
	}{
		{"reported", `{"pid":42,"uptimeMs":90000,"env":{"SECRET":"private-value"},"hostname":"private-host"}`, true},
		{"missing uptime", `{"pid":42}`, false},
		{"invalid pid", `{"pid":0,"uptimeMs":0}`, false},
		{"unsupported", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := rpcFixtureClient(t, func(method string, _ map[string]any) string {
				if method == "system.info" {
					return tc.body
				}
				return `{}`
			})
			data, statuses := collectRuntimeHealth(t.Context(), client, nil)
			if (statuses["runtimeInfo"].State == "ready") != tc.ready {
				t.Fatalf("status=%+v", statuses["runtimeInfo"])
			}
			if tc.ready {
				info := asObj(data["runtimeInfo"])
				if info["pid"] != 42 || info["uptimeMs"] != int64(90000) {
					t.Fatalf("info=%v", info)
				}
			}
			encoded, _ := json.Marshal(data)
			if strings.Contains(string(encoded), "private-") {
				t.Fatalf("sensitive process info: %s", encoded)
			}
		})
	}
}

// agentFailureClient answers per-agent RPCs from failures, so a run can mix
// agents that report with agents the gateway refuses or never answers for.
func agentFailureClient(t *testing.T, failures map[string]string) appopenclaw.Client {
	t.Helper()
	return appopenclaw.Client{Binary: "openclaw", Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		var params map[string]any
		if err := json.Unmarshal([]byte(args[len(args)-1]), &params); err != nil {
			t.Fatal(err)
		}
		agent, _ := params["agentId"].(string)
		switch failures[agent] {
		case "permission_denied":
			return exec.CommandContext(ctx, "sh", "-c", "echo missing scope >&2; exit 1")
		case "timeout":
			cancelled, cancel := context.WithCancel(ctx)
			cancel() // the gateway never gets a chance to answer
			return exec.CommandContext(cancelled, "true")
		}
		body := `{}`
		switch args[2] {
		case "doctor.memory.status":
			body = `{"agentId":"` + agent + `","provider":"ollama","embedding":{"ok":true,"checked":true},"dreaming":{"enabled":false}}`
		case "skills.status":
			body = `{"agentId":"` + agent + `","skills":[{"name":"s"}]}`
		}
		return exec.CommandContext(ctx, "printf", "%s", body)
	}}
}

// TestRuntimeHealthPerAgentFailures guards the honesty invariant for per-agent
// collections: rows from the agents that answered must not be published under a
// blanket "unavailable", and the agents that failed must be named.
func TestRuntimeHealthPerAgentFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  map[string]string
		wantState string
		wantCode  string
		wantRows  int
		wantAgent []string
	}{
		{
			name:      "some agents fail",
			failures:  map[string]string{"b": "permission_denied", "c": "timeout"},
			wantState: "partial", wantCode: "permission_denied", wantRows: 1,
			wantAgent: []string{"b", "c"},
		},
		{
			name:      "only the last agent fails",
			failures:  map[string]string{"c": "timeout"},
			wantState: "partial", wantCode: "timeout", wantRows: 2,
			wantAgent: []string{"c"},
		},
		{
			name:      "every agent fails",
			failures:  map[string]string{"a": "permission_denied", "b": "permission_denied", "c": "timeout"},
			wantState: "unavailable", wantCode: "permission_denied", wantRows: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, statuses := collectRuntimeHealth(t.Context(), agentFailureClient(t, tc.failures), []string{"a", "b", "c"})
			for _, collection := range []string{"memory", "skillInventory"} {
				status := statuses[collection]
				if status.State != tc.wantState || status.Complete {
					t.Fatalf("%s=%+v, want %s and incomplete", collection, status, tc.wantState)
				}
				if status.ErrorCode != tc.wantCode {
					t.Fatalf("%s errorCode=%q, want %q", collection, status.ErrorCode, tc.wantCode)
				}
				if !slices.Equal(status.FailedAgents, tc.wantAgent) {
					t.Fatalf("%s failedAgents=%v, want %v", collection, status.FailedAgents, tc.wantAgent)
				}
			}
			if rows := data["memory"].([]map[string]any); len(rows) != tc.wantRows {
				t.Fatalf("memory rows=%d, want %d", len(rows), tc.wantRows)
			}
			if rows := data["skillInventory"].([]map[string]any); len(rows) != tc.wantRows {
				t.Fatalf("skill rows=%d, want %d", len(rows), tc.wantRows)
			}
		})
	}
}
