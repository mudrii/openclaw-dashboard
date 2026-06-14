package apprefresh

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// loadAgentDefaultModels reads <basePath>/../openclaw.json. seedConfig writes
// the given JSON to the parent of basePath and returns basePath. basePath is a
// child dir of t.TempDir() so "../openclaw.json" resolves into the temp tree.
func seedConfig(t *testing.T, json string) string {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "base")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if json != "" {
		if err := os.WriteFile(filepath.Join(root, "openclaw.json"), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return base
}

func TestLoadAgentDefaultModels(t *testing.T) {
	unknownDefaults := map[string]string{"main": "unknown", "work": "unknown", "group": "unknown"}

	t.Run("missing config file yields unknown defaults", func(t *testing.T) {
		base := seedConfig(t, "") // no openclaw.json written
		got := loadAgentDefaultModels(base)
		if !reflect.DeepEqual(got, unknownDefaults) {
			t.Errorf("got %v, want %v", got, unknownDefaults)
		}
	})

	t.Run("malformed JSON yields unknown defaults", func(t *testing.T) {
		base := seedConfig(t, "{not valid json")
		got := loadAgentDefaultModels(base)
		if !reflect.DeepEqual(got, unknownDefaults) {
			t.Errorf("got %v, want %v", got, unknownDefaults)
		}
	})

	t.Run("per-agent model.primary wins over defaults", func(t *testing.T) {
		cfg := `{
			"agents": {
				"defaults": {"model": {"primary": "anthropic/opus"}},
				"main": {"model": {"primary": "openai/gpt"}},
				"work": {"model": {"primary": "kimi/k2"}},
				"group": {"model": {"primary": "google/gemini"}}
			}
		}`
		got := loadAgentDefaultModels(seedConfig(t, cfg))
		want := map[string]string{
			"main":  "openai/gpt",
			"work":  "kimi/k2",
			"group": "google/gemini",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("agent without model falls back to agents.defaults.model.primary", func(t *testing.T) {
		cfg := `{
			"agents": {
				"defaults": {"model": {"primary": "anthropic/opus"}},
				"main": {"someOtherField": true},
				"work": {"model": {"primary": "kimi/k2"}}
			}
		}`
		got := loadAgentDefaultModels(seedConfig(t, cfg))
		want := map[string]string{
			"main":  "anthropic/opus", // present, no model → defaults.model.primary
			"work":  "kimi/k2",        // present, explicit model
			"group": "anthropic/opus", // absent standard agent → defaults.model.primary
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("defaults key is not emitted as an agent", func(t *testing.T) {
		cfg := `{
			"agents": {
				"defaults": {"model": {"primary": "anthropic/opus"}},
				"main": {"model": {"primary": "openai/gpt"}}
			}
		}`
		got := loadAgentDefaultModels(seedConfig(t, cfg))
		if _, ok := got["defaults"]; ok {
			t.Errorf("defaults key leaked as agent: %v", got)
		}
	})

	t.Run("missing agents.defaults.model.primary makes fallback unknown", func(t *testing.T) {
		// No agents.defaults block at all → primary resolves to "" then is
		// normalized to "unknown", so a model-less agent falls back to "unknown".
		cfg := `{
			"agents": {
				"main": {"model": {"primary": "openai/gpt"}},
				"work": {"noModelHere": true}
			}
		}`
		got := loadAgentDefaultModels(seedConfig(t, cfg))
		want := map[string]string{
			"main":  "openai/gpt",
			"work":  "unknown", // no model + empty primary → "unknown"
			"group": "unknown", // absent → seeded default
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("extra agent beyond main/work/group is included", func(t *testing.T) {
		cfg := `{
			"agents": {
				"defaults": {"model": {"primary": "anthropic/opus"}},
				"main": {"model": {"primary": "openai/gpt"}},
				"research": {"model": {"primary": "perplexity/sonar"}}
			}
		}`
		got := loadAgentDefaultModels(seedConfig(t, cfg))
		// "research" is included as its own key. work/group are absent from the
		// config so they inherit agents.defaults.model.primary.
		want := map[string]string{
			"main":     "openai/gpt",
			"research": "perplexity/sonar",
			"work":     "anthropic/opus",
			"group":    "anthropic/opus",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if _, ok := got["research"]; !ok {
			t.Errorf("extra agent 'research' not included: %v", got)
		}
	})

	t.Run("agents list string and object models populate per-agent defaults", func(t *testing.T) {
		cfg := `{
			"agents": {
				"defaults": {"model": {"primary": "anthropic/opus"}},
				"list": [
					{"id": "main", "model": {"primary": "openai/gpt-5"}},
					{"id": "research", "model": "perplexity/sonar"},
					{"id": "reviewer", "model": {"primary": "anthropic/sonnet"}},
					{"id": "default-agent", "default": true, "model": {"primary": "google/gemini"}}
				]
			}
		}`
		got := loadAgentDefaultModels(seedConfig(t, cfg))
		want := map[string]string{
			"main":          "openai/gpt-5",
			"work":          "anthropic/opus",
			"group":         "anthropic/opus",
			"research":      "perplexity/sonar",
			"reviewer":      "anthropic/sonnet",
			"default-agent": "google/gemini",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}
