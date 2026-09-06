package apprefresh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteSnapshotAtomicConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	const writers = 16
	payloads := make([][]byte, writers)
	known := make(map[string]bool, writers)
	for i := range writers {
		body, err := json.Marshal(map[string]any{"writer": i, "payload": strings.Repeat(string(rune('a'+i)), (i+1)*4096)})
		if err != nil {
			t.Fatal(err)
		}
		payloads[i], known[string(body)] = body, true
	}
	if err := os.WriteFile(path, payloads[0], 0o644); err != nil {
		t.Fatal(err)
	}

	start, stop, readerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	errors := make(chan error, writers+1)
	go func() {
		defer close(readerDone)
		// The writers start only after the reader has observed the initial file.
		first := true
		for {
			body, err := os.ReadFile(path)
			if first {
				close(start)
				first = false
			}
			if err != nil || !known[string(body)] {
				errors <- fmt.Errorf("reader observed incomplete or mixed snapshot (%d bytes): %w", len(body), err)
				return
			}
			select {
			case <-stop:
				return
			default:
			}
		}
	}()
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			<-start
			for range 2 {
				if err := writeSnapshotAtomic(path, payloads[i]); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(stop)
	<-readerDone
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("published mode=%v, want 0600", info.Mode().Perm())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 || files[0].Name() != "data.json" {
		t.Fatalf("temporary files leaked: %v, %v", files, err)
	}
}

func TestWriteSnapshotAtomicRenameFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "snapshot")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "existing")
	if err := os.WriteFile(sentinel, []byte("preserve me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshotAtomic(destination, []byte(`{"new":true}`)); err == nil {
		t.Fatal("rename over a nonempty directory must fail")
	}
	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "preserve me" {
		t.Fatalf("existing destination changed: %q, %v", body, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("failed publication leaked files: %v, %v", files, err)
	}
}
