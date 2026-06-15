package appserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// TestGetDataCached_ConcurrentReadWriteRace exercises the data cache with
// many concurrent readers while a writer mutates the cached map. Without a
// snapshot, callers share the live map with refresh writers and -race fires.
func TestGetDataCached_ConcurrentReadWriteRace(t *testing.T) {
	s := newTestServer(t)

	// Seed data.json so loadData() succeeds without invoking the refresh hook.
	initial := map[string]any{"k": "v0", "n": 0}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "data.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// Prime the cache.
	if _, err := s.GetDataCached(); err != nil {
		t.Fatalf("prime: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: mutate the cached map IN PLACE under the write lock. This
	// models loadData() handing out the same map reference that subsequent
	// refreshes / re-reads then update — readers iterating the prior return
	// value race with the writer's map writes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.dataMu.Lock()
			if s.cachedData != nil {
				s.cachedData["n"] = i
				s.cachedData["x"] = i * 2
			}
			s.dataMu.Unlock()
			i++
		}
	}()

	// Readers: each performs a fixed number of read+iterate cycles, then exits.
	// Bounding by iteration count (not a timed sleep) keeps the race window wide
	// while making the test deterministic and fast.
	const (
		readers    = 64
		readsPerGo = 200
	)
	var readersWG sync.WaitGroup
	for range readers {
		readersWG.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer readersWG.Done()
			for range readsPerGo {
				m, err := s.GetDataCached()
				if err != nil {
					continue
				}
				// Iterate to force map access — this is what fatals under -race
				// when callers share the live cache map with the writer.
				for k, v := range m {
					_ = k
					_ = v
				}
			}
		}()
	}

	// Stop the writer once every reader has finished its bounded run.
	readersWG.Wait()
	close(stop)
	wg.Wait()
}

// TestChatRateLimiter_ConcurrentSameIP exercises chatRateLimiter.allow under
// heavy concurrency from the same IP. The race detector catches any unsynchronised
// access to rateBucket fields during the LoadOrStore → Lock path; the assertion
// pins the contract: exactly chatRateLimit requests are admitted, the rest denied.
func TestChatRateLimiter_ConcurrentSameIP(t *testing.T) {
	var rl chatRateLimiter
	const goroutines = 200
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.allow("10.0.0.1") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != chatRateLimit {
		t.Fatalf("allowed %d requests for one IP, want exactly %d", got, chatRateLimit)
	}
}

// TestChatRateLimiter_ConcurrentManyIPs exercises concurrent access from
// distinct IPs to stress-test the sync.Map insertion path. Each IP issues a
// single request, so all must be admitted and the map must hold one bucket per IP.
func TestChatRateLimiter_ConcurrentManyIPs(t *testing.T) {
	var rl chatRateLimiter
	const goroutines = 200
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ip := "10.0." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256)
			if rl.allow(ip) {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != goroutines {
		t.Fatalf("allowed %d single-request IPs, want all %d", got, goroutines)
	}
	entries := 0
	rl.entries.Range(func(_, _ any) bool {
		entries++
		return true
	})
	if entries != goroutines {
		t.Fatalf("rate limiter holds %d buckets, want one per IP (%d)", entries, goroutines)
	}
}
