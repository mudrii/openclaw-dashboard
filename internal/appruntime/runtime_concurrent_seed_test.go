package appruntime

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestCopyIfMissing_Concurrent verifies CopyIfMissing is race-free when many
// goroutines target the same missing destination. At most one writer should
// produce the file and the result must contain the full source bytes — never
// a partial/corrupt prefix from an interrupted truncate.
func TestCopyIfMissing_Concurrent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "sub", "dst.txt")

	// Use a payload large enough that an unsynchronised truncate+write would
	// almost always be observable as a short read by a racing goroutine.
	payload := make([]byte, 1<<20) // 1 MiB
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, workers)
	)
	wg.Add(workers)
	for i := range workers {
		go func() {
			defer wg.Done()
			<-start
			errs[i] = CopyIfMissing(src, dst, 0o644)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: unexpected error: %v", i, err)
		}
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("partial copy detected: got %d bytes, want %d", len(got), len(payload))
	}
	for i, b := range got {
		if b != payload[i] {
			t.Fatalf("corrupt copy at byte %d: got %d, want %d", i, b, payload[i])
		}
	}
}

// TestCopyFile_AtomicReplace ensures CopyFile replaces dst atomically: a
// concurrent reader observes either the old contents or the new contents,
// never an intermediate truncated/empty state. We exercise the primitive by
// forcing many overwrites while a reader thread samples dst and checks that
// every observation is one of the two valid full payloads.
func TestCopyFile_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	srcOld := filepath.Join(dir, "old.txt")
	srcNew := filepath.Join(dir, "new.txt")
	dst := filepath.Join(dir, "dst.txt")

	oldPayload := make([]byte, 64*1024)
	newPayload := make([]byte, 64*1024)
	for i := range oldPayload {
		oldPayload[i] = 'A'
		newPayload[i] = 'B'
	}
	if err := os.WriteFile(srcOld, oldPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcNew, newPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(srcOld, dst, 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	done := make(chan struct{})
	readerErr := make(chan error, 1)
	go func() {
		defer close(readerErr)
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(dst)
			if err != nil {
				// On a brief race during non-atomic rename it is possible to
				// miss the file entirely. Treat that as a violation too —
				// atomic rename never unlinks the destination.
				readerErr <- err
				return
			}
			if len(data) == 0 {
				readerErr <- &atomicityViolation{kind: "empty file observed"}
				return
			}
			if len(data) != len(oldPayload) {
				readerErr <- &atomicityViolation{kind: "short read", n: len(data)}
				return
			}
			c := data[0]
			if c != 'A' && c != 'B' {
				readerErr <- &atomicityViolation{kind: "garbage byte", b: c}
				return
			}
			for _, b := range data {
				if b != c {
					readerErr <- &atomicityViolation{kind: "mixed contents"}
					return
				}
			}
		}
	}()

	for i := range 50 {
		src := srcNew
		if i%2 == 0 {
			src = srcOld
		}
		if err := CopyFile(src, dst, 0o644); err != nil {
			close(done)
			t.Fatalf("CopyFile iter %d: %v", i, err)
		}
	}
	close(done)
	if err := <-readerErr; err != nil {
		t.Fatalf("atomicity violation: %v", err)
	}
}

type atomicityViolation struct {
	kind string
	n    int
	b    byte
}

func (a *atomicityViolation) Error() string {
	return a.kind
}
