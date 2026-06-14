package apprefresh

import (
	"slices"
	"testing"
)

func TestParseChannels(t *testing.T) {
	t.Run("enabled defaults to true", func(t *testing.T) {
		// A channel object with no "enabled" key is treated as enabled.
		cfg := map[string]any{
			"telegram": map[string]any{"token": "abc"},
		}
		enabled, status := parseChannels(cfg)
		if !slices.Contains(enabled, "telegram") {
			t.Errorf("telegram should be enabled by default, got %v", enabled)
		}
		st := status["telegram"].(map[string]any)
		if st["enabled"] != true {
			t.Errorf("status enabled = %v, want true", st["enabled"])
		}
	})

	t.Run("enabled false omitted from enabled slice", func(t *testing.T) {
		cfg := map[string]any{
			"telegram": map[string]any{"enabled": false, "token": "abc"},
		}
		enabled, status := parseChannels(cfg)
		if slices.Contains(enabled, "telegram") {
			t.Errorf("disabled channel must not be in enabled slice, got %v", enabled)
		}
		// But it must still appear in status (with enabled:false).
		st := status["telegram"].(map[string]any)
		if st["enabled"] != false {
			t.Errorf("status enabled = %v, want false", st["enabled"])
		}
	})

	t.Run("configured inferred from non-reserved keys", func(t *testing.T) {
		// Only reserved keys present → configured false.
		cfg := map[string]any{
			"a": map[string]any{"enabled": true, "connected": true, "health": "ok"},
			"b": map[string]any{"enabled": true, "token": "x"},
		}
		_, status := parseChannels(cfg)
		if status["a"].(map[string]any)["configured"] != false {
			t.Errorf("channel a: configured should be false (only reserved keys)")
		}
		if status["b"].(map[string]any)["configured"] != true {
			t.Errorf("channel b: configured should be true (has token key)")
		}
	})

	t.Run("explicit configured flag honored", func(t *testing.T) {
		cfg := map[string]any{
			"a": map[string]any{"enabled": true, "configured": false, "token": "x"},
		}
		_, status := parseChannels(cfg)
		if status["a"].(map[string]any)["configured"] != false {
			t.Errorf("explicit configured:false should win over inference")
		}
	})

	t.Run("health object connected/error", func(t *testing.T) {
		cfg := map[string]any{
			"a": map[string]any{
				"enabled": true,
				"health":  map[string]any{"connected": true, "error": "warn-msg"},
			},
		}
		_, status := parseChannels(cfg)
		st := status["a"].(map[string]any)
		if st["connected"] != true {
			t.Errorf("connected = %v, want true (from health object)", st["connected"])
		}
		if st["error"] != "warn-msg" {
			t.Errorf("error = %v, want warn-msg (from health object)", st["error"])
		}
	})

	t.Run("health string maps to connected", func(t *testing.T) {
		cfg := map[string]any{
			"online":  map[string]any{"enabled": true, "health": "connected"},
			"offline": map[string]any{"enabled": true, "health": "offline"},
		}
		_, status := parseChannels(cfg)
		if status["online"].(map[string]any)["connected"] != true {
			t.Errorf("health=connected should map connected=true")
		}
		if status["offline"].(map[string]any)["connected"] != false {
			t.Errorf("health=offline should map connected=false")
		}
	})

	t.Run("error falls back to lastError", func(t *testing.T) {
		cfg := map[string]any{
			"a": map[string]any{"enabled": true, "lastError": "stale token"},
		}
		_, status := parseChannels(cfg)
		if status["a"].(map[string]any)["error"] != "stale token" {
			t.Errorf("error should fall back to lastError, got %v", status["a"])
		}
	})

	t.Run("non-object channel entries skipped", func(t *testing.T) {
		cfg := map[string]any{
			"good":   map[string]any{"enabled": true, "token": "x"},
			"scalar": "just-a-string",
		}
		enabled, status := parseChannels(cfg)
		if _, ok := status["scalar"]; ok {
			t.Errorf("scalar channel should be skipped, got %v", status["scalar"])
		}
		if !slices.Contains(enabled, "good") {
			t.Errorf("good channel should be enabled, got %v", enabled)
		}
	})
}

func TestParseAgents(t *testing.T) {
	t.Run("empty list yields synthetic default", func(t *testing.T) {
		agents := map[string]any{}
		out := parseAgents(agents, "openai/gpt-5", nil, map[string]string{"openai/gpt-5": "GPT-5"}, nil)
		if len(out) != 1 {
			t.Fatalf("want 1 synthetic default, got %d", len(out))
		}
		a := out[0].(map[string]any)
		if a["id"] != "default" || a["role"] != "Default" || a["isDefault"] != true {
			t.Errorf("synthetic default fields mismatch: %v", a)
		}
		if a["model"] != "GPT-5" || a["modelId"] != "openai/gpt-5" {
			t.Errorf("synthetic default model mismatch: %v", a)
		}
	})

	t.Run("model object vs string", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"id": "obj", "model": map[string]any{"primary": "anthropic/claude-sonnet"}},
				map[string]any{"id": "str", "model": "openai/gpt-5"},
			},
		}
		aliases := map[string]string{"anthropic/claude-sonnet": "Sonnet", "openai/gpt-5": "GPT-5"}
		out := parseAgents(agents, "openai/gpt-5", nil, aliases, nil)
		if out[0].(map[string]any)["modelId"] != "anthropic/claude-sonnet" {
			t.Errorf("object model primary not used: %v", out[0])
		}
		if out[1].(map[string]any)["modelId"] != "openai/gpt-5" {
			t.Errorf("string model not used: %v", out[1])
		}
	})

	t.Run("missing id becomes agent-N", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"role": "Worker"},
			},
		}
		out := parseAgents(agents, "openai/gpt-5", nil, map[string]string{}, nil)
		if out[0].(map[string]any)["id"] != "agent-0" {
			t.Errorf("missing id should become agent-0, got %v", out[0].(map[string]any)["id"])
		}
	})

	t.Run("role derivation from id when absent", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"id": "code-reviewer"},
			},
		}
		out := parseAgents(agents, "openai/gpt-5", nil, map[string]string{}, nil)
		// id "code-reviewer" → "Code reviewer" (dashes→spaces, TitleCase first byte).
		if out[0].(map[string]any)["role"] != "Code reviewer" {
			t.Errorf("role = %v, want Code reviewer", out[0].(map[string]any)["role"])
		}
	})

	t.Run("default agent role is Default", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"id": "main", "default": true},
			},
		}
		out := parseAgents(agents, "openai/gpt-5", nil, map[string]string{}, nil)
		a := out[0].(map[string]any)
		if a["role"] != "Default" || a["isDefault"] != true {
			t.Errorf("default agent role mismatch: %v", a)
		}
	})

	t.Run("fallbacks capped at 3 and aliased", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"id": "a", "model": map[string]any{
					"primary":   "openai/gpt-5",
					"fallbacks": []any{"m1", "m2", "m3", "m4"},
				}},
			},
		}
		aliases := map[string]string{"m1": "Model1"}
		out := parseAgents(agents, "openai/gpt-5", nil, aliases, nil)
		fb := out[0].(map[string]any)["fallbacks"].([]string)
		if len(fb) != 3 {
			t.Fatalf("fallbacks should cap at 3, got %d: %v", len(fb), fb)
		}
		// m1 aliased; m2/m3 pass through raw.
		if fb[0] != "Model1" || fb[1] != "m2" || fb[2] != "m3" {
			t.Errorf("fallback aliasing mismatch: %v", fb)
		}
	})

	t.Run("inherits global fallbacks when agent has none", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"id": "a", "model": "openai/gpt-5"},
			},
		}
		global := []string{"g1", "g2"}
		out := parseAgents(agents, "openai/gpt-5", global, map[string]string{}, nil)
		fb := out[0].(map[string]any)["fallbacks"].([]string)
		if !slices.Equal(fb, []string{"g1", "g2"}) {
			t.Errorf("should inherit global fallbacks, got %v", fb)
		}
	})
}

func TestParseBindings(t *testing.T) {
	t.Run("peer and match fields carried", func(t *testing.T) {
		oc := map[string]any{
			"bindings": []any{
				map[string]any{
					"agentId": "worker",
					"match": map[string]any{
						"channel":   "telegram",
						"accountId": "acc-1",
						"guildId":   "g-1",
						"teamId":    "t-1",
						"peer":      map[string]any{"kind": "user", "id": "u-1"},
					},
				},
			},
		}
		out := parseBindings(oc, map[string]any{})
		// out[0] is the parsed binding, out[len-1] is the appended default.
		b := out[0].(map[string]any)
		if b["agentId"] != "worker" || b["channel"] != "telegram" {
			t.Errorf("binding base fields mismatch: %v", b)
		}
		if b["kind"] != "user" || b["id"] != "u-1" {
			t.Errorf("peer kind/id not carried: %v", b)
		}
		if b["accountId"] != "acc-1" || b["guildId"] != "g-1" || b["teamId"] != "t-1" {
			t.Errorf("account/guild/team not carried: %v", b)
		}
	})

	t.Run("non-object bindings skipped", func(t *testing.T) {
		oc := map[string]any{
			"bindings": []any{"scalar", map[string]any{"agentId": "real"}},
		}
		out := parseBindings(oc, map[string]any{})
		// one real binding + one default = 2.
		if len(out) != 2 {
			t.Fatalf("want 2 (1 real + default), got %d: %v", len(out), out)
		}
		if out[0].(map[string]any)["agentId"] != "real" {
			t.Errorf("first binding mismatch: %v", out[0])
		}
	})

	t.Run("default entry appended with main fallback", func(t *testing.T) {
		out := parseBindings(map[string]any{}, map[string]any{})
		if len(out) != 1 {
			t.Fatalf("want just the default entry, got %d", len(out))
		}
		d := out[0].(map[string]any)
		if d["kind"] != "default" || d["channel"] != "all" || d["agentId"] != "main" {
			t.Errorf("default entry mismatch: %v", d)
		}
	})

	t.Run("default agent pulled from agents.list default:true", func(t *testing.T) {
		agents := map[string]any{
			"list": []any{
				map[string]any{"id": "primary", "default": true},
				map[string]any{"id": "other"},
			},
		}
		out := parseBindings(map[string]any{}, agents)
		d := out[len(out)-1].(map[string]any)
		if d["agentId"] != "primary" {
			t.Errorf("default agentId = %v, want primary (from list default)", d["agentId"])
		}
	})
}

func TestDefaultAgentConfig(t *testing.T) {
	got := defaultAgentConfig()

	// Wire contract: the full skeleton key set must be present.
	wantKeys := []string{
		"primaryModel", "primaryModelId", "imageModel", "imageModelId",
		"fallbacks", "streamMode", "telegramDmPolicy", "telegramGroups",
		"channels", "channelStatus", "compaction", "agents", "search",
		"gateway", "hooks", "plugins", "skills", "bindings", "crons",
		"tts", "diagnostics", "contextWindow", "maxOutputTokens", "memoryPolicy",
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q in default agent config", k)
		}
	}

	// Empty slices/maps must be non-nil (so they marshal to [] / {} not null).
	if fb, ok := got["fallbacks"].([]string); !ok || fb == nil {
		t.Errorf("fallbacks should be non-nil empty []string, got %T %v", got["fallbacks"], got["fallbacks"])
	}
	if ch, ok := got["channels"].([]string); !ok || ch == nil {
		t.Errorf("channels should be non-nil empty []string, got %T", got["channels"])
	}
	if cs, ok := got["channelStatus"].(map[string]any); !ok || cs == nil {
		t.Errorf("channelStatus should be non-nil empty map, got %T", got["channelStatus"])
	}
	if ag, ok := got["agents"].([]any); !ok || ag == nil {
		t.Errorf("agents should be non-nil empty []any, got %T", got["agents"])
	}

	// Scalar limits are nil (the dashboard surfaces null, not a fabricated default).
	if got["contextWindow"] != nil {
		t.Errorf("contextWindow should be nil, got %v", got["contextWindow"])
	}
	if got["maxOutputTokens"] != nil {
		t.Errorf("maxOutputTokens should be nil, got %v", got["maxOutputTokens"])
	}
	if got["memoryPolicy"] != nil {
		t.Errorf("memoryPolicy should be nil, got %v", got["memoryPolicy"])
	}
}
