package apprefresh

import "testing"

// D2: prefer channels.telegram.streaming.mode over legacy streamMode.
func TestTelegramStreamMode_PrefersNestedOverLegacy(t *testing.T) {
	tg := map[string]any{
		"streamMode": "off",
		"streaming":  map[string]any{"mode": "full"},
	}
	if got := telegramStreamMode(tg); got != "full" {
		t.Errorf("want full (nested wins), got %q", got)
	}
}

func TestTelegramStreamMode_FallsBackToLegacy(t *testing.T) {
	tg := map[string]any{"streamMode": "partial"}
	if got := telegramStreamMode(tg); got != "partial" {
		t.Errorf("legacy fallback: want partial, got %q", got)
	}
}

func TestTelegramStreamMode_DefaultWhenAbsent(t *testing.T) {
	if got := telegramStreamMode(map[string]any{}); got != "off" {
		t.Errorf("default: want off, got %q", got)
	}
}

// D3: plugins.entries.<id>.enabled is surfaced in the output.
func TestParsePlugins_SurfacesEnabledState(t *testing.T) {
	oc := map[string]any{
		"plugins": map[string]any{
			"entries": map[string]any{
				"alpha": map[string]any{"enabled": false},
				"beta":  map[string]any{}, // default true
				"gamma": map[string]any{"enabled": true},
			},
		},
	}
	got := parsePlugins(oc)
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	want := []struct {
		name    string
		enabled bool
	}{{"alpha", false}, {"beta", true}, {"gamma", true}}
	for i, w := range want {
		if got[i]["name"] != w.name {
			t.Errorf("idx %d: name = %v, want %v", i, got[i]["name"], w.name)
		}
		if got[i]["enabled"] != w.enabled {
			t.Errorf("idx %d (%s): enabled = %v, want %v", i, w.name, got[i]["enabled"], w.enabled)
		}
	}
}

// D7: parseBindings extracts accountId, guildId, teamId from match block.
func TestParseBindings_CarriesAccountGuildTeam(t *testing.T) {
	oc := map[string]any{
		"bindings": []any{
			map[string]any{
				"agentId": "work",
				"match": map[string]any{
					"channel":   "slack",
					"accountId": "acme",
					"teamId":    "T01",
					"peer":      map[string]any{"kind": "channel", "id": "C0123"},
				},
			},
			map[string]any{
				"agentId": "ops",
				"match": map[string]any{
					"channel": "discord",
					"guildId": "G55",
					"peer":    map[string]any{"kind": "channel", "id": "999"},
				},
			},
		},
	}
	agents := map[string]any{"list": []any{}}
	out := parseBindings(oc, agents)
	// out has the two bindings + a "default" entry appended at the end.
	if len(out) != 3 {
		t.Fatalf("want 3 entries (2 + default), got %d", len(out))
	}
	a := out[0].(map[string]any)
	if a["accountId"] != "acme" || a["teamId"] != "T01" || a["guildId"] != "" {
		t.Errorf("slack binding missing fields: %+v", a)
	}
	b := out[1].(map[string]any)
	if b["guildId"] != "G55" || b["accountId"] != "" || b["teamId"] != "" {
		t.Errorf("discord binding missing fields: %+v", b)
	}
}
