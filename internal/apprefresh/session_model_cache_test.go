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
	started := make(chan struct{})
	release := make(chan struct{})
	c := newLiveSessionModelCache()
	c.fetchFn = func(ctx context.Context, _ func(ctx context.Context, name string, args ...string) *exec.Cmd, _ string) map[string]string {
		mu.Lock()
		calls++
		if calls == 1 {
			close(started)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-ctx.Done():
			return map[string]string{}
		}
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
	<-started
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 fetch under contention, got %d", calls)
	}
}

// TestLiveSessionModelCache_PanicDoesNotHangWaiters guards the refreshAndStore
// defer: if the in-flight fetch panics, refreshing must be cleared and waiters
// woken so cond.Wait() callers retry instead of blocking forever. Without the
// fix, the first refresher panics with refreshing stuck true and every waiter
// deadlocks — this test would then time out.
func TestLiveSessionModelCache_PanicDoesNotHangWaiters(t *testing.T) {
	t.Parallel()

	c := newLiveSessionModelCache()
	c.fetchFn = func(ctx context.Context, _ func(ctx context.Context, name string, args ...string) *exec.Cmd, _ string) map[string]string {
		panic("simulated fetch failure")
	}
	c.resolveOpenclaw = func() string { return "x" }

	now := time.Now()
	const callers = 8
	done := make(chan struct{}, callers)
	for range callers {
		go func() {
			// Each caller recovers its own propagated panic; the contract under
			// test is that no caller hangs, not that the panic is suppressed.
			defer func() {
				_ = recover()
				done <- struct{}{}
			}()
			_ = c.fetch(context.Background(), now, time.Hour)
		}()
	}

	deadline := time.After(5 * time.Second)
	for i := 0; i < callers; i++ {
		select {
		case <-done:
		case <-deadline:
			t.Fatalf("waiter hung: only %d/%d callers returned — refreshing not cleared on panic", i, callers)
		}
	}
}
