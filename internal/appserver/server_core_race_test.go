package appserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

	// Readers: iterate over the returned map (the racy operation).
	const readers = 64
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
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

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
}
