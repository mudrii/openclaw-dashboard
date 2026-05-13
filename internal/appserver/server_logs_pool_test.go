package appserver

// Decision: no worker pool added.
//
// The actual file I/O for log sources happens inside
// apprefresh.ReadMergedLogs (see internal/apprefresh/logtail.go).
// In appserver, s.readMergedLogs is a one-line delegation and never
// opens files itself.
//
// Sequential reads in apprefresh already cap FD usage at 1 at any
// moment (each file is opened, scanned, and closed via defer before
// the next source is touched). The original "could exhaust FD limit"
// concern does not apply to sequential code paths.
//
// Adding parallelism would only meaningfully help if implemented at
// the apprefresh layer where the opens occur. The task scope limits
// edits to internal/appserver/server_logs.go, and reimplementing the
// read+parse+merge pipeline there would duplicate apprefresh logic
// and defeat the existing heap-merge across sources. With typical
// configured source counts (~5-10), the speedup is marginal anyway.
//
// What this file contributes instead is behavior coverage for
// readMergedLogs at the appserver seam with many sources and the
// empty-sources edge case.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
)

func TestReadMergedLogs_ManySources(t *testing.T) {
	openclawDir := t.TempDir()
	const numSources = 20
	const entriesPerSource = 5

	sources := make([]string, 0, numSources)
	for src := range numSources {
		rel := filepath.Join("logs", "src"+strconv.Itoa(src)+".log")
		sources = append(sources, rel)
		lines := make([]string, 0, entriesPerSource)
		for entry := range entriesPerSource {
			// Distinct timestamps: source index controls minutes,
			// entry index controls seconds. Every entry across all
			// sources gets a unique timestamp.
			line := fmt.Sprintf("2026-04-13T10:%02d:%02dZ msg src=%d entry=%d", src, entry, src, entry)
			lines = append(lines, line)
		}
		writeLines(t, filepath.Join(openclawDir, rel), lines...)
	}

	s := &Server{openclawPath: openclawDir}
	want := numSources * entriesPerSource
	records, err := s.readMergedLogs(sources, want)
	if err != nil {
		t.Fatalf("readMergedLogs failed: %v", err)
	}
	if len(records) != want {
		t.Fatalf("expected %d records, got %d", want, len(records))
	}

	// Verify ascending timestamp order (merge-sorted output).
	for i := 1; i < len(records); i++ {
		if records[i].TimestampMs < records[i-1].TimestampMs {
			t.Fatalf("records not in ascending order at i=%d: %d < %d",
				i, records[i].TimestampMs, records[i-1].TimestampMs)
		}
	}

	// Verify every source contributed all of its entries.
	seenBySource := make(map[string]int, numSources)
	for _, r := range records {
		seenBySource[r.Source]++
	}
	if len(seenBySource) != numSources {
		t.Fatalf("expected %d distinct sources in output, got %d", numSources, len(seenBySource))
	}
	for src, count := range seenBySource {
		if count != entriesPerSource {
			t.Fatalf("source %q contributed %d entries, want %d", src, count, entriesPerSource)
		}
	}
}

func TestReadMergedLogs_EmptySources(t *testing.T) {
	openclawDir := t.TempDir()
	s := &Server{openclawPath: openclawDir}

	// Defensive: must not panic, must return empty result.
	records, err := s.readMergedLogs(nil, 100)
	if err != nil {
		t.Fatalf("nil sources: unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("nil sources: expected 0 records, got %d", len(records))
	}

	records, err = s.readMergedLogs([]string{}, 100)
	if err != nil {
		t.Fatalf("empty sources: unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("empty sources: expected 0 records, got %d", len(records))
	}
}
