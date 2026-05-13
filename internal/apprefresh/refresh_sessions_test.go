package apprefresh

import (
	"testing"
)

func TestBuildGroupNames_ExtractsSubject(t *testing.T) {
	stores := []SessionStoreFile{{
		AgentName: "work",
		Store: map[string]map[string]any{
			"agent:work:group:42:main": {"subject": "My Team"},
			"agent:work:group:43:main": {"displayName": "Other Team"},
			// Should be skipped: contains topic
			"agent:work:group:44:topic:foo": {"subject": "Topic Sub"},
			// Should be skipped: telegram-prefixed name
			"agent:work:group:45:main": {"subject": "telegram:thing"},
		},
	}}
	out := buildGroupNames(stores)
	if out["42"] != "My Team" {
		t.Errorf("gid=42: want %q, got %q", "My Team", out["42"])
	}
	if out["43"] != "Other Team" {
		t.Errorf("gid=43: want %q, got %q", "Other Team", out["43"])
	}
	if _, ok := out["44"]; ok {
		t.Errorf("gid=44 (topic) should be skipped")
	}
	if _, ok := out["45"]; ok {
		t.Errorf("gid=45 (telegram-prefixed) should be skipped")
	}
}

func TestEnrichBindings_FillsNames(t *testing.T) {
	agentConfig := map[string]any{
		"bindings": []any{
			map[string]any{"id": "42", "name": ""},
			map[string]any{"id": "99", "name": ""},
			map[string]any{"id": "", "name": ""},
		},
	}
	enrichBindings(agentConfig, map[string]string{"42": "My Team"})

	bindings := agentConfig["bindings"].([]any)
	if got := bindings[0].(map[string]any)["name"]; got != "My Team" {
		t.Errorf("binding 42 name: want %q, got %q", "My Team", got)
	}
	if got := bindings[1].(map[string]any)["name"]; got != "" {
		t.Errorf("binding 99 (unmapped) name: want empty, got %q", got)
	}
	if got := bindings[2].(map[string]any)["name"]; got != "" {
		t.Errorf("binding empty-id name: want empty, got %q", got)
	}
}

func TestEnrichBindings_NoBindingsField(t *testing.T) {
	// Should not panic on missing/invalid bindings.
	enrichBindings(map[string]any{}, map[string]string{"42": "X"})
	enrichBindings(map[string]any{"bindings": "not-a-slice"}, map[string]string{"42": "X"})
}

func TestBackfillChannelConnectivity_MarksActive(t *testing.T) {
	sessions := []map[string]any{
		{"key": "agent:work:telegram:123:main", "active": true},
		{"key": "agent:main:cron:42:main", "active": true}, // ignored: cron
		{"key": "agent:main:subagent:5:main", "active": true}, // ignored: subagent
		{"key": "agent:work:whatsapp:7:main", "active": false}, // inactive
	}
	agentConfig := map[string]any{
		"channelStatus": map[string]any{
			"telegram": map[string]any{"connected": nil, "health": nil},
			"whatsapp": map[string]any{"connected": nil, "health": nil},
			"slack":    map[string]any{"connected": true, "health": "ok"},
		},
	}
	backfillChannelConnectivity(agentConfig, sessions)

	cs := agentConfig["channelStatus"].(map[string]any)
	tg := cs["telegram"].(map[string]any)
	if tg["connected"] != true {
		t.Errorf("telegram connected: want true, got %v", tg["connected"])
	}
	if tg["health"] != "active" {
		t.Errorf("telegram health: want active, got %v", tg["health"])
	}
	wa := cs["whatsapp"].(map[string]any)
	if wa["connected"] != nil {
		t.Errorf("whatsapp should remain unset (no active session), got %v", wa["connected"])
	}
	sl := cs["slack"].(map[string]any)
	if sl["connected"] != true || sl["health"] != "ok" {
		t.Errorf("slack pre-existing values must not be overwritten, got %+v", sl)
	}
}
