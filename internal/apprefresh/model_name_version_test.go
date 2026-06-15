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
		"openai/gpt-4.1":       "GPT-4.1",       // gpt-4 family also preserves version
		"openai/gpt-4o":        "GPT-4o",        // gpt-4o special case still wins
		"openai/gpt-4":         "GPT-4",         // gpt-4 base unchanged
	}
	for model, want := range cases {
		if got := ModelName(model); got != want {
			t.Errorf("ModelName(%q) = %q, want %q", model, got, want)
		}
	}
}

// TestModelName_ClaudeFamilyVersion locks the Claude curated fallback to keep
// the full version (claude-opus-4-7 → Claude Opus 4.7) instead of collapsing to
// a hardcoded 4.5, and to surface sonnet/haiku versions too — matching the
// version-preserving treatment of the GLM/GPT families.
func TestModelName_ClaudeFamilyVersion(t *testing.T) {
	prevN := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prevN) })
	modelCatalogNames.Store(nil)

	cases := map[string]string{
		"anthropic/claude-opus-4-6":          "Claude Opus 4.6",
		"anthropic/claude-opus-4-7":          "Claude Opus 4.7",
		"anthropic/claude-sonnet-4-6":        "Claude Sonnet 4.6",
		"anthropic/claude-haiku-4-5":         "Claude Haiku 4.5",
		"anthropic/opus":                     "Claude Opus", // no version segment
		"claude-sonnet":                      "Claude Sonnet",
		"anthropic/claude-opus-4-5-20260514": "Claude Opus 4.5", // 8-digit date snapshot dropped
		"anthropic/claude-opus-4-thinking":   "Claude Opus 4",   // word suffix stops the version
		"anthropic/claude-opus-thinking":     "Claude Opus",     // no numeric version at all
	}
	for model, want := range cases {
		if got := ModelName(model); got != want {
			t.Errorf("ModelName(%q) = %q, want %q", model, got, want)
		}
	}
}

// TestModelName_GeminiTierNeutralFallback guards that an unrecognized Gemini id
// is not mislabeled as the Flash tier — a Pro/Ultra release with no catalog name
// must fall back to the tier-neutral "Gemini", while an explicit flash id still
// renders "Gemini Flash".
func TestModelName_GeminiTierNeutralFallback(t *testing.T) {
	prevN := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prevN) })
	modelCatalogNames.Store(nil)

	cases := map[string]string{
		"google/gemini-3-pro":   "Gemini",       // unknown Pro must NOT become Flash
		"google/gemini-4":       "Gemini",       // future bare Gemini
		"google/gemini-9-flash": "Gemini Flash", // explicit flash still Flash
		"google/gemini-2.5-pro": "Gemini 2.5 Pro",
	}
	for model, want := range cases {
		if got := ModelName(model); got != want {
			t.Errorf("ModelName(%q) = %q, want %q", model, got, want)
		}
	}
}

// TestModelName_KimiTierNeutralFallback guards that an unrecognized Kimi id is
// not pinned to "Kimi K2.5" — only the k2p5 id maps there; other Kimi ids fall
// back to the tier-neutral "Kimi".
func TestModelName_KimiTierNeutralFallback(t *testing.T) {
	prevN := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prevN) })
	modelCatalogNames.Store(nil)

	cases := map[string]string{
		"kimi/k2p5":     "Kimi K2.5", // canonical id keeps the curated name
		"kimi/kimi-k3":  "Kimi",      // future id must not be pinned to K2.5
		"moonshot/kimi": "Kimi",
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
