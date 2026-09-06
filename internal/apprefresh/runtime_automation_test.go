package apprefresh

import (
	"testing"
	"time"
)

func TestRuntimeAutomationPayloadAndDelivery(t *testing.T) {
	for _, kind := range []string{"agentTurn", "systemEvent", "heartbeat", "command", "script", "skillCollectionReview"} {
		t.Run(kind, func(t *testing.T) {
			job := map[string]any{"id": "job", "enabled": false, "sessionKey": "owner", "sessionTarget": "isolated", "payload": map[string]any{"kind": kind, "message": "private prompt"}, "delivery": map[string]any{"mode": "announce", "channel": "telegram", "to": "private target"}, "schedule": map[string]any{"kind": "cron", "expr": "0 3 * * *", "tz": "UTC"}}
			row := cronJobToMap(job, nil, time.UTC)
			if row["payloadKind"] != kind || row["sessionKey"] != "owner" || row["status"] != "disabled" || row["timezone"] != "UTC" {
				t.Fatalf("job=%v", row)
			}
			if row["payload"] != nil || asObj(row["delivery"])["to"] != nil {
				t.Fatal("unnecessary payload/destination exposed")
			}
		})
	}
}

func TestRuntimeAutomationPaginationAndStructuredDiagnostics(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if method != "cron.list" || params["includeDisabled"] != true {
			t.Fatalf("query=%s %v", method, params)
		}
		if params["offset"] == float64(0) {
			return `{"jobs":[{"id":"one","enabled":false,"payload":{"kind":"command"},"configRevision":"rev","owner":{"agentId":"main","sessionKey":"owner"},"state":{"lastDiagnostics":{"summary":"failed","entries":[{"message":"token=secret-value","severity":"error"}]}}}],"hasMore":true,"nextOffset":1,"total":2}`
		}
		return `{"jobs":[{"id":"two","enabled":true,"state":{"runningAtMs":100}}],"hasMore":false,"total":2}`
	})
	rows, err := collectRuntimeCrons(t.Context(), client, time.UTC)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if rows[0]["configRevision"] != "rev" || rows[1]["status"] != "running" {
		t.Fatalf("rows=%v", rows)
	}
	diagnostics := rows[0]["lastDiagnostics"].([]string)
	if len(diagnostics) != 2 || diagnostics[1] == "token=secret-value" {
		t.Fatalf("diagnostics=%v", diagnostics)
	}
}
