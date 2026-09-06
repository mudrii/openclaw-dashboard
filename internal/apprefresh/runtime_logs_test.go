package apprefresh

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeLogsBoundedAndRedacted(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if method != "logs.tail" || params["limit"] != float64(50) || params["maxBytes"] != float64(262144) {
			t.Fatalf("query=%s %v", method, params)
		}
		return `{"lines":["2026-09-05T00:00:00Z [error] token=secret-value Authorization: Bearer bearer-value"],"cursor":123,"truncated":true,"reset":false}`
	})
	result, err := ReadRuntimeLogs(t.Context(), client, 50)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "secret-value") || strings.Contains(string(data), "bearer-value") {
		t.Fatalf("credential exposed: %s", data)
	}
	if len(result.Entries) != 1 || !result.Truncated || result.Cursor != 123 || result.Entries[0].Source != "gateway" {
		t.Fatalf("result=%+v", result)
	}
}
