package apprefresh

import (
	"testing"
)

// TestLookupModelLimits pins the lookup against the openclaw.json registry shape
// (models.providers[provider].models[]). Returns nil for unknown ids; correct
// values when the id matches one of the provider's model entries.
func TestLookupModelLimits(t *testing.T) {
	oc := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"minimax": map[string]any{
					"models": []any{
						map[string]any{"id": "MiniMax-M2.7", "contextWindow": 196608, "maxTokens": 16384},
						map[string]any{"id": "MiniMax-M2.5", "contextWindow": 196608, "maxTokens": 16384},
					},
				},
				"kimi": map[string]any{
					"models": []any{
						map[string]any{"id": "k2p5", "contextWindow": 131072, "maxTokens": 8192},
					},
				},
			},
		},
	}

	t.Run("known model returns limits", func(t *testing.T) {
		ctx, max := lookupModelLimits(oc, "minimax/MiniMax-M2.7")
		if ctx != 196608 || max != 16384 {
			t.Errorf("want (196608, 16384), got (%v, %v)", ctx, max)
		}
	})

	t.Run("unknown provider returns nil", func(t *testing.T) {
		ctx, max := lookupModelLimits(oc, "fake-vendor/model-x")
		if ctx != nil || max != nil {
			t.Errorf("want (nil, nil), got (%v, %v)", ctx, max)
		}
	})

	t.Run("unknown model id under known provider returns nil", func(t *testing.T) {
		ctx, max := lookupModelLimits(oc, "minimax/does-not-exist")
		if ctx != nil || max != nil {
			t.Errorf("want (nil, nil), got (%v, %v)", ctx, max)
		}
	})

	t.Run("empty id returns nil", func(t *testing.T) {
		ctx, max := lookupModelLimits(oc, "")
		if ctx != nil || max != nil {
			t.Errorf("want (nil, nil), got (%v, %v)", ctx, max)
		}
	})

	t.Run("id without slash returns nil", func(t *testing.T) {
		ctx, max := lookupModelLimits(oc, "no-provider-slash")
		if ctx != nil || max != nil {
			t.Errorf("want (nil, nil), got (%v, %v)", ctx, max)
		}
	})

	t.Run("kimi/k2p5 returns its limits", func(t *testing.T) {
		ctx, max := lookupModelLimits(oc, "kimi/k2p5")
		if ctx != 131072 || max != 8192 {
			t.Errorf("want (131072, 8192), got (%v, %v)", ctx, max)
		}
	})
}

// TestParseMemoryPolicy verifies the projection of agents.defaults.memorySearch
// into the agentConfig.memoryPolicy wire shape.
func TestParseMemoryPolicy(t *testing.T) {
	t.Run("nil when block absent", func(t *testing.T) {
		got := parseMemoryPolicy(map[string]any{})
		if got != nil {
			t.Errorf("want nil, got %v", got)
		}
	})

	t.Run("populated when memorySearch present", func(t *testing.T) {
		defaults := map[string]any{
			"memorySearch": map[string]any{
				"enabled":  true,
				"sources":  []any{"memory", "sessions"},
				"fallback": "none",
				"experimental": map[string]any{
					"sessionMemory": true,
				},
			},
		}
		got := parseMemoryPolicy(defaults)
		if got == nil {
			t.Fatalf("want non-nil")
		}
		if got["enabled"] != true {
			t.Errorf("enabled: want true, got %v", got["enabled"])
		}
		srcs, ok := got["sources"].([]any)
		if !ok || len(srcs) != 2 {
			t.Errorf("sources: want []any of len 2, got %v", got["sources"])
		}
		if got["fallback"] != "none" {
			t.Errorf("fallback: want none, got %v", got["fallback"])
		}
		exp, ok := got["experimental"].(map[string]any)
		if !ok || exp["sessionMemory"] != true {
			t.Errorf("experimental.sessionMemory: want true, got %v", got["experimental"])
		}
	})

	t.Run("populated without experimental block", func(t *testing.T) {
		defaults := map[string]any{
			"memorySearch": map[string]any{
				"enabled": false,
			},
		}
		got := parseMemoryPolicy(defaults)
		if got == nil {
			t.Fatalf("want non-nil")
		}
		if got["enabled"] != false {
			t.Errorf("enabled: want false, got %v", got["enabled"])
		}
		if _, ok := got["experimental"]; ok {
			t.Errorf("experimental: want absent, got %v", got["experimental"])
		}
	})
}

// TestParseOpenclawConfig_SurfacesPrimaryModelLimits proves the full
// agentConfig now carries contextWindow + maxOutputTokens + memoryPolicy
// pulled from the corresponding openclaw.json blocks. This is the
// integration-level check that the Phase B parser additions wired through
// end-to-end.
func TestParseOpenclawConfig_SurfacesPrimaryModelLimits(t *testing.T) {
	oc := map[string]any{
		"agents": map[string]any{
			"defaults": map[string]any{
				"model": map[string]any{"primary": "minimax/MiniMax-M2.7"},
				"models": map[string]any{
					"minimax/MiniMax-M2.7": map[string]any{"alias": "MiniMax M2.7"},
				},
				"memorySearch": map[string]any{
					"enabled": true,
					"sources": []any{"memory", "sessions"},
				},
			},
		},
		"models": map[string]any{
			"providers": map[string]any{
				"minimax": map[string]any{
					"models": []any{
						map[string]any{"id": "MiniMax-M2.7", "contextWindow": 196608, "maxTokens": 16384},
					},
				},
			},
		},
	}
	_, _, _, _, ac := parseOpenclawConfig(oc, "")
	if ac["contextWindow"] != 196608 {
		t.Errorf("contextWindow: want 196608, got %v", ac["contextWindow"])
	}
	if ac["maxOutputTokens"] != 16384 {
		t.Errorf("maxOutputTokens: want 16384, got %v", ac["maxOutputTokens"])
	}
	mp, ok := ac["memoryPolicy"].(map[string]any)
	if !ok {
		t.Fatalf("memoryPolicy: want map, got %T", ac["memoryPolicy"])
	}
	if mp["enabled"] != true {
		t.Errorf("memoryPolicy.enabled: want true, got %v", mp["enabled"])
	}
}
