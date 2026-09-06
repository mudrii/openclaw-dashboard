package apprefresh

import "testing"

func TestRuntimeConfigCurrentKeysAndEffectiveAgent(t *testing.T) {
	oc := map[string]any{
		"memory": map[string]any{"search": map[string]any{"enabled": true, "provider": "ollama", "model": "embed", "remote": map[string]any{"apiKey": "secret"}}, "dreaming": map[string]any{"enabled": true}},
		"agents": map[string]any{"defaults": map[string]any{"model": "p/a", "workspace": "/effective/workspace", "modelPolicy": map[string]any{"allow": []any{"p/a", "p/b"}}, "memorySearch": map[string]any{"enabled": false}, "models": map[string]any{"p/a": map[string]any{"alias": "Primary"}, "p/c": map[string]any{}}}},
	}
	_, _, models, _, config := parseOpenclawConfig(oc, "/state/agents")
	policy := asObj(config["memoryPolicy"])
	if policy["enabled"] != true || policy["provider"] != "ollama" || policy["remote"] != nil {
		t.Fatalf("policy=%v", policy)
	}
	if len(models) != 2 {
		t.Fatalf("allowed models=%v", models)
	}
	agents := config["agents"].([]any)
	main := asObj(agents[0])
	if main["id"] != "main" || main["workspace"] != "/effective/workspace" {
		t.Fatalf("agent=%v", main)
	}
	if config["dreaming"] == nil {
		t.Fatal("dreaming not projected")
	}
}
