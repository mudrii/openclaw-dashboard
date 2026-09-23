package appsystem

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

var statusOutputSeeds = []string{
	`{"service":{"loaded":true,"runtime":{"status":"running","pid":123}},"version":"2026.3.1"}`,
	"[INFO] starting {wrong} {\"service\":{\"loaded\":false},\"rpc\":{\"ok\":true,\"server\":{\"version\":\"1.2\"}}}\ntrailing log",
	`{"currentVersion":"1","latestVersion":"2","connectLatencyMs":12.5,"tasks":{"total":1},"eventLoop":null,"channelSummary":["a","",3]}`,
	`{"runtimeVersion":"3","heartbeat":{"at":1},"tasks":"bad"}`,
	"Service loaded, running",
	"not loaded",
	"{",
	"",
}

// FuzzDecodeJSONObjectFromOutput checks the preamble-skipping decoder never
// panics and, when output is itself one strict JSON object, decodes it as
// encoding/json does.
func FuzzDecodeJSONObjectFromOutput(f *testing.F) {
	for _, s := range statusOutputSeeds {
		f.Add(s)
	}
	discardSlog(f)
	f.Fuzz(func(t *testing.T, out string) {
		var got map[string]any
		err := decodeJSONObjectFromOutput(out, &got)
		trimmed := strings.TrimSpace(out)
		if !strings.HasPrefix(trimmed, "{") || !json.Valid([]byte(trimmed)) {
			return
		}
		var want map[string]any
		if json.Unmarshal([]byte(trimmed), &want) != nil {
			return // valid syntax but undecodable, e.g. a float overflow
		}
		if err != nil {
			t.Fatalf("rejected strict object %q: %v", out, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("decode differs for %q\ngot  %#v\nwant %#v", out, got, want)
		}
	})
}

// FuzzParseStatusOutputs runs arbitrary CLI output through the status and
// gateway-status parsers. The container target keeps ParseGatewayStatusJSON
// from shelling out to ps for a fuzzed PID.
func FuzzParseStatusOutputs(f *testing.F) {
	for _, s := range statusOutputSeeds {
		f.Add(s)
	}
	discardSlog(f)
	ctx := appopenclaw.WithTarget(context.Background(), appopenclaw.Target{Container: "fuzz"})
	f.Fuzz(func(t *testing.T, out string) {
		gw := ParseGatewayStatusJSON(ctx, out)
		if !slices.Contains([]string{"online", "offline", "unknown"}, gw.Status) {
			t.Fatalf("gateway status %q for %q", gw.Status, out)
		}
		if gw.PID != 0 || gw.Uptime != "" || gw.Memory != "" {
			t.Fatalf("container target kept process info %+v", gw)
		}
		status, err := parseOpenclawStatusJSON(out, SystemVersions{Openclaw: "v", Latest: "l"})
		if err != nil {
			return
		}
		if _, err := json.Marshal(status); err != nil {
			t.Fatalf("parsed status not JSON-encodable: %v", err)
		}
		_ = normalizeCLIVersion(out)
	})
}

// discardSlog silences the parsers' debug/warn logging for a fuzz run and
// restores the previous default logger afterwards.
func discardSlog(f *testing.F) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Cleanup(func() { slog.SetDefault(prev) })
}
