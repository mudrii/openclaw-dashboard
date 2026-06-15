package apprefresh

import "testing"

// TestModelName_PreservesFamilyVersion locks the fix for version-collapsing:
// the GLM/GPT curated fallback must keep the full minor version (GLM-5.2, not
// GLM-5; GPT-5.5, not GPT-5). OpenClaw's catalog emits these ids as their own
// bare-id "name" (e.g. "glm-5.2"), which catalogNameIsBareID routes to this
// fallback — so the fallback, not the catalog, decides the rendered version.
func TestModelName_PreservesFamilyVersion(t *testing.T) {
	// Pure curated path: no catalog snapshot published.
	prevN := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prevN) })
	modelCatalogNames.Store(nil)

	cases := map[string]string{
		"zai/glm-5.2":          "GLM-5.2",
		"zai/glm-5.1":          "GLM-5.1",
		"zai/glm-5":            "GLM-5", // base version unchanged
		"zai/glm-4.7":          "GLM-4.7",
		"zai/glm-4":            "GLM-4", // base version unchanged
		"openai/gpt-5.5":       "GPT-5.5",
		"openai/gpt-5.4":       "GPT-5.4",
		"openai/gpt-5":         "GPT-5",         // base version unchanged
		"openai/gpt-5.3-codex": "GPT-5.3 Codex", // explicit case still wins
	}
	for model, want := range cases {
		if got := ModelName(model); got != want {
			t.Errorf("ModelName(%q) = %q, want %q", model, got, want)
		}
	}
}

// TestModelName_GenuineCatalogNameStillWins guards that a real friendly catalog
// name (not a bare id) is still preferred over the curated fallback.
func TestModelName_GenuineCatalogNameStillWins(t *testing.T) {
	prevN := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prevN) })
	setModelCatalogNames(map[string]string{"zai/glm-5.2": "GLM 5.2 Turbo"})

	if got := ModelName("zai/glm-5.2"); got != "GLM 5.2 Turbo" {
		t.Errorf("ModelName = %q, want GLM 5.2 Turbo (genuine catalog name wins)", got)
	}
}
