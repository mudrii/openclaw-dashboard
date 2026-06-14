package apprefresh

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// TestParseModelCatalog covers the `openclaw models list --json` parser: it maps
// each row's key → display name, tolerates the bare-array form, and ignores rows
// without a usable name. The id field is `key` (no separate provider/alias).
func TestParseModelCatalog(t *testing.T) {
	t.Run("envelope form", func(t *testing.T) {
		out := `{"count":2,"models":[
			{"key":"anthropic/claude-opus-4-6","name":"Claude Opus 4.6","contextWindow":200000},
			{"key":"google/gemini-3.1-pro-preview","name":"Gemini 3.1 Pro","contextTokens":1048576}
		]}`
		names := parseModelCatalog([]byte(out))
		if names["anthropic/claude-opus-4-6"] != "Claude Opus 4.6" {
			t.Errorf("opus name = %q", names["anthropic/claude-opus-4-6"])
		}
		if names["google/gemini-3.1-pro-preview"] != "Gemini 3.1 Pro" {
			t.Errorf("gemini name = %q", names["google/gemini-3.1-pro-preview"])
		}
	})
	t.Run("bare array form", func(t *testing.T) {
		out := `[{"key":"x/y","name":"Fancy Model"}]`
		names := parseModelCatalog([]byte(out))
		if names["x/y"] != "Fancy Model" {
			t.Errorf("name = %q, want Fancy Model", names["x/y"])
		}
	})
	t.Run("row without name skipped", func(t *testing.T) {
		out := `{"models":[{"key":"a/b"},{"key":"c/d","name":"Dee"}]}`
		names := parseModelCatalog([]byte(out))
		if _, ok := names["a/b"]; ok {
			t.Errorf("row without name must be skipped, got %q", names["a/b"])
		}
		if names["c/d"] != "Dee" {
			t.Errorf("name = %q, want Dee", names["c/d"])
		}
	})
	t.Run("malformed yields empty", func(t *testing.T) {
		if names := parseModelCatalog([]byte(`{not json`)); len(names) != 0 {
			t.Errorf("malformed → %v, want empty", names)
		}
	})
}

// TestModelName_ConsultsCatalog proves ModelName prefers a live catalog display
// name over the hardcoded switch, and falls back to the switch on a miss so
// every existing mapping still works when the catalog is empty/unavailable.
func TestModelName_ConsultsCatalog(t *testing.T) {
	prev := modelCatalogNames.Load()
	t.Cleanup(func() { modelCatalogNames.Store(prev) })

	setModelCatalogNames(map[string]string{"vendor/brand-new-9000": "Brand New 9000"})
	if got := ModelName("vendor/brand-new-9000"); got != "Brand New 9000" {
		t.Errorf("catalog hit: got %q, want Brand New 9000", got)
	}
	// Miss → hardcoded switch still applies.
	if got := ModelName("anthropic/claude-opus-4-6"); got != "Claude Opus 4.6" {
		t.Errorf("catalog miss fallback: got %q, want Claude Opus 4.6", got)
	}
}

// TestModelCatalogCache_FetchWithStubRunner exercises the TTL cache end to end
// with a stubbed CLI runner: the parsed names are returned and the snapshot is
// published for ModelName.
func TestModelCatalogCache_FetchWithStubRunner(t *testing.T) {
	c := newModelCatalogCache()
	c.resolveOpenclaw = func() string { return "openclaw" }
	c.runner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// Echo a fixed catalog regardless of args.
		return exec.CommandContext(ctx, "printf", `%s`,
			`{"models":[{"key":"m/n","name":"Em En"}]}`)
	}
	names := c.fetch(context.Background(), time.Now(), time.Minute)
	if names["m/n"] != "Em En" {
		t.Fatalf("fetch names = %v, want m/n=Em En", names)
	}
}

// TestModelCatalogCache_RunnerErrorYieldsEmpty proves a failing CLI collapses to
// an empty catalog (ModelName then relies entirely on the hardcoded switch).
func TestModelCatalogCache_RunnerErrorYieldsEmpty(t *testing.T) {
	c := newModelCatalogCache()
	c.resolveOpenclaw = func() string { return "openclaw" }
	c.runner = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false") // exits non-zero, no output
	}
	if names := c.fetch(context.Background(), time.Now(), time.Minute); len(names) != 0 {
		t.Errorf("runner error → %v, want empty", names)
	}
}
