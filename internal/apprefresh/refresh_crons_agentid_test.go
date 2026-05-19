package apprefresh

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCollectCrons_SurfacesIdAndAgentId pins the new projection fields: each
// cron entry now carries the job's stable id and its agentId so the dashboard
// can render a per-agent cron column. The existing fields (name, model,
// schedule, status...) are unchanged.
func TestCollectCrons_SurfacesIdAndAgentId(t *testing.T) {
	dir := t.TempDir()
	jobsPath := filepath.Join(dir, "jobs.json")

	const jobsJSON = `{
  "jobs": [
    {
      "id": "ed947712-65dd-4d0a-9544-533469cdb7a2",
      "name": "Main Agent: Daily Memory Curation",
      "agentId": "main",
      "enabled": true,
      "schedule": { "kind": "cron", "expr": "0 23 * * *" },
      "state": { "lastRunStatus": "ok", "lastRunAtMs": 1779116400000, "nextRunAtMs": 1779202800000, "lastDurationMs": 344773 },
      "payload": { "model": "kimi/k2p5" }
    },
    {
      "id": "fb47d50f-9183-4ca5-bc3a-c03758b1cb46",
      "name": "Biz Agent: Daily Memory Curation",
      "agentId": "biz",
      "enabled": false,
      "schedule": { "kind": "cron", "expr": "45 23 * * *" },
      "state": { "lastRunStatus": "error", "lastRunAtMs": 1779119100000 },
      "payload": { "model": "kimi/k2p5" }
    }
  ]
}`
	if err := os.WriteFile(jobsPath, []byte(jobsJSON), 0o600); err != nil {
		t.Fatalf("write jobs: %v", err)
	}

	got := CollectCrons(jobsPath, time.UTC)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}

	type want struct {
		id      string
		name    string
		agentID string
		enabled bool
		model   string
		status  string
	}
	wants := []want{
		{
			id:      "ed947712-65dd-4d0a-9544-533469cdb7a2",
			name:    "Main Agent: Daily Memory Curation",
			agentID: "main",
			enabled: true,
			model:   "kimi/k2p5",
			status:  "ok",
		},
		{
			id:      "fb47d50f-9183-4ca5-bc3a-c03758b1cb46",
			name:    "Biz Agent: Daily Memory Curation",
			agentID: "biz",
			enabled: false,
			model:   "kimi/k2p5",
			status:  "error",
		},
	}

	for i, w := range wants {
		c := got[i]
		if c["id"] != w.id {
			t.Errorf("row %d id: want %q, got %v", i, w.id, c["id"])
		}
		if c["name"] != w.name {
			t.Errorf("row %d name: want %q, got %v", i, w.name, c["name"])
		}
		if c["agentId"] != w.agentID {
			t.Errorf("row %d agentId: want %q, got %v", i, w.agentID, c["agentId"])
		}
		if c["enabled"] != w.enabled {
			t.Errorf("row %d enabled: want %v, got %v", i, w.enabled, c["enabled"])
		}
		if c["model"] != w.model {
			t.Errorf("row %d model: want %q, got %v", i, w.model, c["model"])
		}
		if c["lastStatus"] != w.status {
			t.Errorf("row %d lastStatus: want %q, got %v", i, w.status, c["lastStatus"])
		}
	}
}

// TestCollectCrons_MissingAgentIdEmptyString defends against the case where a
// legacy job has no agentId — the field must still be present (empty string)
// so the frontend can render a uniform shape.
func TestCollectCrons_MissingAgentIdEmptyString(t *testing.T) {
	dir := t.TempDir()
	jobsPath := filepath.Join(dir, "jobs.json")

	const jobsJSON = `{
  "jobs": [
    {
      "id": "legacy-no-agent",
      "name": "Legacy Job",
      "enabled": true,
      "schedule": { "kind": "cron", "expr": "0 0 * * *" },
      "state": { "lastRunStatus": "ok" }
    }
  ]
}`
	if err := os.WriteFile(jobsPath, []byte(jobsJSON), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := CollectCrons(jobsPath, time.UTC)
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	if got[0]["agentId"] != "" {
		t.Errorf("agentId: want empty string, got %v", got[0]["agentId"])
	}
	if got[0]["id"] != "legacy-no-agent" {
		t.Errorf("id: want %q, got %v", "legacy-no-agent", got[0]["id"])
	}
}
