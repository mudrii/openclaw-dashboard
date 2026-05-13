package apprefresh

import "testing"

// Malformed keys with an empty group id (e.g. "group::main") must not
// produce an entry in the output map. Such input previously produced an
// empty-string key, which downstream code would silently match.
func TestBuildGroupNames_SkipsEmptyGID(t *testing.T) {
	stores := []SessionStoreFile{{
		AgentName: "work",
		Store: map[string]map[string]any{
			"agent:work:group::main":   {"subject": "Should Not Appear"},
			"agent:work:group:42:main": {"subject": "Real Team"},
		},
	}}

	out := buildGroupNames(stores)

	if _, ok := out[""]; ok {
		t.Errorf("empty gid must not be added: got %q", out[""])
	}
	if out["42"] != "Real Team" {
		t.Errorf("gid=42: want %q, got %q", "Real Team", out["42"])
	}
}
