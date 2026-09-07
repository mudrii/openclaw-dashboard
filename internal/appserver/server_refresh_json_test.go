package appserver

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
)

// newDataServer writes body to data.json in a fresh temp dir and returns a
// Server rooted there.
func newDataServer(t *testing.T, body []byte) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json"), body, 0o600); err != nil {
		t.Fatalf("write data.json: %v", err)
	}
	return NewServer(dir, "1.0.0", appconfig.Default(), "", []byte("<html></html>"), t.Context(), nil)
}

// TestDashboardPayloadFixtureIsRealistic guards the premise of both the decode
// equivalence test and the appserver benchmarks: the fixture must be close to a
// production data.json in size and key count, otherwise those measurements
// degenerate into syscall timing.
func TestDashboardPayloadFixtureIsRealistic(t *testing.T) {
	raw := marshalDashboardPayload(t)
	if len(raw) < 30_000 {
		t.Errorf("fixture too small: got %d bytes, want >= 30000", len(raw))
	}
	if got := len(buildDashboardPayload()); got != 37 {
		t.Errorf("top-level key count: got %d, want 37", got)
	}
}

// TestLoadDataDecodeMatchesJSONV1 pins the contract of the encoding/json/v2
// switch in loadData: for a payload produced by the production writer, strict
// v2 decoding yields a value indistinguishable from the v1 decoding the
// dashboard used before.
func TestLoadDataDecodeMatchesJSONV1(t *testing.T) {
	raw := marshalDashboardPayload(t)

	var viaV1 map[string]any
	if err := json.Unmarshal(raw, &viaV1); err != nil {
		t.Fatalf("v1 unmarshal: %v", err)
	}
	var viaV2 map[string]any
	if err := jsonv2.Unmarshal(raw, &viaV2); err != nil {
		t.Fatalf("v2 unmarshal: %v", err)
	}

	t.Run("v1 and v2 agree", func(t *testing.T) {
		if !reflect.DeepEqual(viaV1, viaV2) {
			t.Error("v1 and v2 decodes of the production-writer payload differ")
		}
	})

	t.Run("loadData agrees", func(t *testing.T) {
		s := newDataServer(t, raw)
		gotRaw, parsed, err := s.loadData()
		if err != nil {
			t.Fatalf("loadData: %v", err)
		}
		if !reflect.DeepEqual(gotRaw, raw) {
			t.Error("loadData returned different raw bytes than were written")
		}
		if !reflect.DeepEqual(parsed, viaV1) {
			t.Error("loadData parse differs from encoding/json v1 decode")
		}
	})
}

// TestLoadDataRejectsDuplicateMembers documents the stricter contract adopted
// with encoding/json/v2: data.json is written by this program, so a duplicate
// object member means the file is corrupt and must be surfaced as an error
// rather than silently resolved last-key-wins the way v1 did.
func TestLoadDataRejectsDuplicateMembers(t *testing.T) {
	const dup = `{"botName":"a","botName":"b","lastRefresh":"2026-01-01"}`

	t.Run("encoding/json v1 accepts it", func(t *testing.T) {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(dup), &parsed); err != nil {
			t.Fatalf("v1 unmarshal: %v", err)
		}
		if parsed["botName"] != "b" {
			t.Errorf("v1 last-key-wins: got %v, want b", parsed["botName"])
		}
	})

	t.Run("loadData rejects it", func(t *testing.T) {
		s := newDataServer(t, []byte(dup))
		if _, parsed, err := s.loadData(); err == nil {
			t.Fatalf("loadData accepted duplicate object member, parsed = %v", parsed)
		}
	})
}
