package apprefresh

import (
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestRetainLastGoodCollection(t *testing.T) {
	for _, tt := range []struct {
		name, state, path string
		wantStale         bool
	}{
		{"failed same target", "unavailable", "/state", true},
		{"empty success replaces rows", "ready", "/state", false},
		{"different state never reused", "unavailable", "/other", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := appopenclaw.Target{Mode: "native"}
			current := map[string]any{"runtimeTarget": target, "stateDir": tt.path, "sessions": []any{}, "collections": map[string]CollectionStatus{"sessions": {Source: "gateway.sessions.list", State: tt.state, AttemptedAt: "new"}}}
			previous := map[string]any{"runtimeTarget": map[string]any{"mode": "native"}, "stateDir": "/state", "sessions": []any{map[string]any{"key": "s"}}, "collections": map[string]any{"sessions": map[string]any{"source": "gateway.sessions.list", "state": "ready", "collectedAt": "old"}}}
			retainLastGoodCollections(current, previous)
			meta := current["collections"].(map[string]CollectionStatus)["sessions"]
			if (meta.State == "stale") != tt.wantStale {
				t.Fatalf("metadata=%+v", meta)
			}
			if tt.wantStale && (meta.CollectedAt != "old" || meta.AttemptedAt != "new" || len(current["sessions"].([]any)) != 1) {
				t.Fatalf("data=%v", current)
			}
		})
	}
}

func TestRuntimeInfoRetentionStaysOnSelectedTarget(t *testing.T) {
	for _, oldMode := range []string{"native", "container"} {
		t.Run(oldMode, func(t *testing.T) {
			current := map[string]any{"runtimeTarget": appopenclaw.Target{Mode: "container", Container: "selected"}, "collections": map[string]CollectionStatus{"runtimeInfo": {Source: "gateway.system.info", State: "unavailable"}}}
			previous := map[string]any{"runtimeTarget": appopenclaw.Target{Mode: oldMode, Container: "selected"}, "runtimeInfo": map[string]any{"pid": 42}, "collections": map[string]any{"runtimeInfo": map[string]any{"source": "gateway.system.info", "collectedAt": "old"}}}
			retainLastGoodCollections(current, previous)
			if (current["runtimeInfo"] != nil) != (oldMode == "container") {
				t.Fatalf("retained=%v", current)
			}
			if oldMode == "container" && current["collections"].(map[string]CollectionStatus)["runtimeInfo"].State != "stale" {
				t.Fatal("missing stale marker")
			}
		})
	}
}

func TestRetainCompleteUsageDuringWarming(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Kuala_Lumpur"} {
		t.Run(zone, func(t *testing.T) {
			current := map[string]any{"stateDir": "/s", "runtimeTarget": appopenclaw.Target{}, "timezone": zone, "tokenUsage": []any{}, "collections": map[string]CollectionStatus{"usageAll": {Source: "gateway.sessions.usage", State: "partial", AttemptedAt: "now"}}}
			previous := map[string]any{"stateDir": "/s", "runtimeTarget": map[string]any{}, "timezone": "UTC", "tokenUsage": []any{"prior"}, "collections": map[string]any{"usageAll": map[string]any{"source": "gateway.sessions.usage", "collectedAt": "then", "complete": true}}}
			retainLastGoodCollections(current, previous)
			got := current["collections"].(map[string]CollectionStatus)["usageAll"]
			if (got.State == "stale") != (zone == "UTC") {
				t.Fatalf("metadata=%+v", got)
			}
		})
	}
}

func TestRepeatedWarmingRetainsPreviouslyCompleteUsage(t *testing.T) {
	current := map[string]any{"runtimeTarget": appopenclaw.Target{}, "usageAll": map[string]any{"totalTokens": 0}, "collections": map[string]CollectionStatus{"usageAll": {Source: "gateway.sessions.usage", State: "partial"}}}
	previous := map[string]any{"runtimeTarget": map[string]any{}, "usageAll": map[string]any{"totalTokens": 100, "tokensComplete": true}, "collections": map[string]any{"usageAll": map[string]any{"source": "gateway.sessions.usage", "state": "stale", "complete": false, "collectedAt": "old"}}}
	retainLastGoodCollections(current, previous)
	if asObj(current["usageAll"])["totalTokens"] != 100 || current["collections"].(map[string]CollectionStatus)["usageAll"].State != "stale" {
		t.Fatalf("lost good data: %v", current)
	}
}

func TestConfigurationRetentionRequiresSameRuntimeAndSource(t *testing.T) {
	for _, tc := range []struct {
		name, container, source string
		retain                  bool
	}{
		{"same container", "selected", "gateway.config.get", true},
		{"other container", "other", "gateway.config.get", false},
		{"host config source", "selected", "native.config.file", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := map[string]any{"runtimeTarget": appopenclaw.Target{Mode: "container", Container: "selected"}, "collections": map[string]CollectionStatus{"configuration": {Source: "gateway.config.get", State: "unavailable"}}}
			previous := map[string]any{"runtimeTarget": appopenclaw.Target{Mode: "container", Container: tc.container}, "agentConfig": "prior", "compactionMode": "safeguard", "collections": map[string]any{"configuration": map[string]any{"source": tc.source, "collectedAt": "old"}}}
			retainLastGoodCollections(current, previous)
			if (current["agentConfig"] == "prior") != tc.retain {
				t.Fatalf("unexpected configuration retention: %v", current)
			}
			if (current["compactionMode"] == "safeguard") != tc.retain {
				t.Fatalf("compaction summary does not match retained configuration: %v", current)
			}
		})
	}
}
