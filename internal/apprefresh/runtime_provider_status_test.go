package apprefresh

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeProviderStatusProjection(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, _ map[string]any) string {
		switch method {
		case "health":
			return `{"ok":true,"ts":123,"channels":{"private":"secret-value"}}`
		case "usage.status":
			return `{"updatedAt":123,"providers":[{"provider":"minimax","displayName":"MiniMax","plan":"Coding Plan","windows":[{"label":"5h","usedPercent":0,"resetAt":456,"token":"secret-value"}],"billing":[{"type":"balance","amount":0,"unit":"USD","key":"secret-value"}],"apiKey":"secret-value"}]}`
		default:
			t.Fatalf("unexpected method %s", method)
			return `{}`
		}
	})
	data, statuses := collectRuntimeProviderStatus(t.Context(), client)
	if statuses["gatewayHealth"].State != "ready" || statuses["providerUsage"].State != "ready" {
		t.Fatalf("statuses=%v", statuses)
	}
	if asObj(data["gatewayHealth"])["ok"] != true {
		t.Fatal("health result lost")
	}
	encoded, _ := json.Marshal(data)
	if strings.Contains(string(encoded), "secret-value") {
		t.Fatalf("credentials leaked: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"usedPercent":0`) || !strings.Contains(string(encoded), `"amount":0`) {
		t.Fatalf("zero values lost: %s", encoded)
	}
}

func TestRuntimeProviderStatusMissingAndUnhealthy(t *testing.T) {
	for _, tc := range []struct{ name, health, usage, state string }{
		{"missing", `{}`, `{}`, "unavailable"},
		{"unhealthy", `{"ok":false}`, `{"providers":[]}`, "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := rpcFixtureClient(t, func(method string, _ map[string]any) string {
				if method == "health" {
					return tc.health
				}
				return tc.usage
			})
			data, statuses := collectRuntimeProviderStatus(t.Context(), client)
			if statuses["gatewayHealth"].State != tc.state || statuses["providerUsage"].State != tc.state {
				t.Fatalf("statuses=%v", statuses)
			}
			if tc.name == "unhealthy" && asObj(data["gatewayHealth"])["ok"] != false {
				t.Fatal("unhealthy gateway fabricated healthy")
			}
		})
	}
}

func TestRuntimeHealthRPCErrorIsNotAnUnhealthyVerdict(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, _ map[string]any) string {
		if method == "health" {
			return `{"ok":false,"error":"unauthorized"}`
		}
		return `{"providers":[]}`
	})
	data, statuses := collectRuntimeProviderStatus(t.Context(), client)
	if statuses["gatewayHealth"].State != "unavailable" || data["gatewayHealth"] != nil {
		t.Fatalf("error treated as health result: %v %v", data, statuses)
	}
}
