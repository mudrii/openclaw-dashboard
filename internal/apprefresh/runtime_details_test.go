package apprefresh

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeWorkContextExcludesWidgetTicketsAndHTML(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if params["sessionKey"] != "session" {
			t.Fatalf("params=%v", params)
		}
		if method == "progressCard.get" {
			return `{"card":{"revision":3,"markdown":"<script>bad</script>","steps":[{"step":"verify","status":"in_progress"}]}}`
		}
		return `{"sessionKey":"session","revision":2,"tabs":[{"tabId":"main","title":"Main"}],"widgets":[{"name":"view","title":"Widget","contentKind":"html","grantState":"pending","viewTicket":"secret-ticket","frameUrl":"secret-url","props":{"html":"dangerous-html"}}]}`
	})
	result := ReadSessionWork(t.Context(), client, "session")
	b, _ := json.Marshal(result)
	if strings.Contains(string(b), "secret-ticket") || strings.Contains(string(b), "secret-url") || strings.Contains(string(b), "dangerous-html") {
		t.Fatalf("widget credentials/content leaked: %s", b)
	}
	if len(result["widgets"].([]map[string]any)) != 1 {
		t.Fatalf("result=%v", result)
	}
}

func TestRuntimeAutomationHistoryRetainsPaginationAndDelivery(t *testing.T) {
	client := rpcFixtureClient(t, func(method string, params map[string]any) string {
		if method != "cron.runs" || params["id"] != "job" || params["offset"] != float64(50) || params["limit"] != float64(50) {
			t.Fatalf("query=%s %v", method, params)
		}
		return `{"entries":[{"jobId":"job","runAtMs":1,"status":"ok","completionStatus":"completed","deliveryStatus":"not-requested","durationMs":20}],"hasMore":true,"nextOffset":100,"total":150}`
	})
	result, err := ReadAutomationRuns(t.Context(), client, "job", 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	if result["hasMore"] != true || result["nextOffset"] != 100 {
		t.Fatalf("result=%v", result)
	}
	rows := result["entries"].([]map[string]any)
	if rows[0]["deliveryStatus"] != "not-requested" {
		t.Fatalf("entries=%v", rows)
	}
}
