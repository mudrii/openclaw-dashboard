package apprefresh

import (
	"encoding/json"
	"strings"
	"testing"
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
