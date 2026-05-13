package apprefresh

import (
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Isolated per-instance cache enables t.Parallel() without globals contention.
func TestLiveSessionModelCache_IsolatedInstances(t *testing.T) {
	t.Parallel()

	makeCache := func(model string) *liveSessionModelCache {
		c := newLiveSessionModelCache()
		c.fetchFn = func(ctx context.Context, _ func(ctx context.Context, name string, args ...string) *exec.Cmd, _ string) map[string]string {
			return map[string]string{"agent:main:chat": model}
		}
		c.resolveOpenclaw = func() string { return "/fake/openclaw" }
		return c
	}

	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	a := makeCache("GPT-5")
	b := makeCache("Claude")

	gotA := a.fetch(context.Background(), now, 30*time.Second)
	gotB := b.fetch(context.Background(), now, 30*time.Second)

	if gotA["agent:main:chat"] != "GPT-5" {
		t.Errorf("cache A: got %q, want GPT-5", gotA["agent:main:chat"])
	}
	if gotB["agent:main:chat"] != "Claude" {
		t.Errorf("cache B: got %q, want Claude", gotB["agent:main:chat"])
	}
}

// Singleflight invariant: under concurrent contention only one fetch runs.
func TestLiveSessionModelCache_Singleflight(t *testing.T) {
	t.Parallel()

	var calls int
	var mu sync.Mutex
	c := newLiveSessionModelCache()
	c.fetchFn = func(ctx context.Context, _ func(ctx context.Context, name string, args ...string) *exec.Cmd, _ string) map[string]string {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return map[string]string{"k": "v"}
	}
	c.resolveOpenclaw = func() string { return "x" }

	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.fetch(context.Background(), now, time.Hour)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 fetch under contention, got %d", calls)
	}
}
