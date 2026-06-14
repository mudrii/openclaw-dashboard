package apprefresh

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// TestModelCatalogCache_Singleflight proves the TTL cache collapses concurrent
// refreshes into a single underlying fetch and hands every waiter the same
// catalog — exercising the cond.Wait waiter path.
func TestModelCatalogCache_Singleflight(t *testing.T) {
	t.Parallel()

	var calls int
	var mu sync.Mutex
	started := make(chan struct{})
	release := make(chan struct{})
	c := newModelCatalogCache()
	c.resolveOpenclaw = func() string { return "x" }
	c.runner = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		mu.Lock()
		calls++
		if calls == 1 {
			close(started)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return exec.CommandContext(ctx, "printf", `{"models":[{"key":"m/n","name":"Em En"}]}`)
	}

	now := time.Now()
	results := make([]modelCatalog, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = c.fetch(context.Background(), now, time.Hour)
		}(i)
	}
	<-started
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 fetch under contention, got %d", calls)
	}
	for i, r := range results {
		if r.names["m/n"] != "Em En" {
			t.Errorf("waiter %d got %v, want m/n=Em En", i, r.names)
		}
	}
}

// TestParseModelCatalog covers the `openclaw models list --json` parser: it maps
// each row's key → display name, tolerates the bare-array form, and ignores rows
// without a usable name. The id field is `key` (no separate provider/alias).
func TestParseModelCatalog(t *testing.T) {
	t.Run("envelope form: names and context windows", func(t *testing.T) {
		out := `{"count":2,"models":[
			{"key":"anthropic/claude-opus-4-6","name":"Claude Opus 4.6","contextWindow":200000},
			{"key":"google/gemini-3.1-pro-preview","name":"Gemini 3.1 Pro","contextTokens":1048576}
		]}`
		cat := parseModelCatalog([]byte(out))
		if cat.names["anthropic/claude-opus-4-6"] != "Claude Opus 4.6" {
			t.Errorf("opus name = %q", cat.names["anthropic/claude-opus-4-6"])
		}
		if cat.windows["anthropic/claude-opus-4-6"] != 200000 {
			t.Errorf("opus window = %d, want 200000", cat.windows["anthropic/claude-opus-4-6"])
		}
		// contextTokens used when contextWindow absent.
		if cat.windows["google/gemini-3.1-pro-preview"] != 1048576 {
			t.Errorf("gemini window = %d, want 1048576 (from contextTokens)", cat.windows["google/gemini-3.1-pro-preview"])
		}
	})
	t.Run("bare array form", func(t *testing.T) {
		cat := parseModelCatalog([]byte(`[{"key":"x/y","name":"Fancy Model"}]`))
		if cat.names["x/y"] != "Fancy Model" {
			t.Errorf("name = %q, want Fancy Model", cat.names["x/y"])
		}
	})
	t.Run("row without name skipped; window still captured", func(t *testing.T) {
		cat := parseModelCatalog([]byte(`{"models":[{"key":"a/b","contextWindow":4096},{"key":"c/d","name":"Dee"}]}`))
		if _, ok := cat.names["a/b"]; ok {
			t.Errorf("row without name must be skipped from names, got %q", cat.names["a/b"])
		}
		if cat.windows["a/b"] != 4096 {
			t.Errorf("window a/b = %d, want 4096 (captured even without a name)", cat.windows["a/b"])
		}
		if cat.names["c/d"] != "Dee" {
			t.Errorf("name = %q, want Dee", cat.names["c/d"])
		}
	})
	t.Run("malformed yields empty", func(t *testing.T) {
		cat := parseModelCatalog([]byte(`{not json`))
		if len(cat.names) != 0 || len(cat.windows) != 0 {
			t.Errorf("malformed → %+v, want empty", cat)
		}
	})
}

// TestLookupModelLimits_CatalogFallback proves the live catalog supplies a
// context window when the openclaw.json registry has no entry for the model
// (stock configs that rely on the internal catalog), while the registry value
// still wins when present.
func TestLookupModelLimits_CatalogFallback(t *testing.T) {
	prevW := modelCatalogWindows.Load()
	t.Cleanup(func() { modelCatalogWindows.Store(prevW) })
	setModelCatalogWindows(map[string]int{"vendor/new-model": 262144})

	// Registry has no providers block → miss → catalog supplies the window.
	cw, _ := lookupModelLimits(map[string]any{}, "vendor/new-model")
	if cw != 262144 {
		t.Errorf("context window = %v, want 262144 from catalog fallback", cw)
	}

	// Registry value wins over catalog.
	oc := map[string]any{"models": map[string]any{"providers": map[string]any{
		"vendor": map[string]any{"models": []any{
			map[string]any{"id": "new-model", "contextWindow": float64(99999)},
		}},
	}}}
	cw2, _ := lookupModelLimits(oc, "vendor/new-model")
	if cw2 != float64(99999) {
		t.Errorf("context window = %v, want 99999 from registry (registry wins)", cw2)
	}
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
	cat := c.fetch(context.Background(), time.Now(), time.Minute)
	if cat.names["m/n"] != "Em En" {
		t.Fatalf("fetch names = %v, want m/n=Em En", cat.names)
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
	if cat := c.fetch(context.Background(), time.Now(), time.Minute); len(cat.names) != 0 {
		t.Errorf("runner error → %v, want empty", cat.names)
	}
}
