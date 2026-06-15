package apprefresh

import "testing"

// TestResolveUsageModel locks the Token Usage panel's model display to the same
// rule the Sessions panel uses: resolve any alias, then prettify through
// ModelName — so an aliased model shows "GLM-5.2", not the raw lowercase
// "glm-5.2", while a genuine custom alias passes through unchanged.
func TestResolveUsageModel(t *testing.T) {
	prev := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prev) })
	modelCatalogNames.Store(nil)

	t.Run("no alias prettifies via ModelName", func(t *testing.T) {
		if got := resolveUsageModel("zai/glm-5.2", nil); got != "GLM-5.2" {
			t.Errorf("got %q, want GLM-5.2", got)
		}
	})
	t.Run("lowercase alias is prettified for parity with sessions", func(t *testing.T) {
		got := resolveUsageModel("x/y", map[string]string{"x/y": "glm-5.2"})
		if got != "GLM-5.2" {
			t.Errorf("got %q, want GLM-5.2 (alias must be prettified, not shown raw)", got)
		}
	})
	t.Run("genuine custom alias passes through", func(t *testing.T) {
		got := resolveUsageModel("x/y", map[string]string{"x/y": "My Fancy Model"})
		if got != "My Fancy Model" {
			t.Errorf("got %q, want My Fancy Model (custom alias unchanged)", got)
		}
	})
}
