package apprefresh

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

const runtimePageSize = 100
const runtimeMaxRows = 1000

type CollectionStatus struct {
	Source      string `json:"source"`
	State       string `json:"state"`
	ErrorCode   string `json:"errorCode,omitempty"`
	CollectedAt string `json:"collectedAt,omitempty"`
	AttemptedAt string `json:"attemptedAt"`
	Complete    bool   `json:"complete"`
}

func collectionStatus(source string, err error, complete bool) CollectionStatus {
	stamp := time.Now().UTC().Format(time.RFC3339)
	status := CollectionStatus{Source: source, State: "ready", CollectedAt: stamp, AttemptedAt: stamp, Complete: complete}
	if !complete {
		status.State = "partial"
	}
	if err != nil {
		status.State = "unavailable"
		status.ErrorCode = appopenclaw.ErrorCode(err)
		status.CollectedAt = ""
		status.Complete = false
	}
	return status
}

func hasRuntimeState(basePath string, target appopenclaw.Target) bool {
	if target.Mode != "" || target.IsContainer() || target.Profile != "" {
		return true
	}
	files, _ := filepath.Glob(filepath.Join(basePath, "*", "agent", "openclaw-agent.sqlite"))
	if len(files) > 0 {
		return true
	}
	_, err := os.Stat(filepath.Join(filepath.Dir(basePath), "state", "openclaw.sqlite"))
	return err == nil
}

// UsesRuntimeData identifies migrated or explicitly selected installations.
func UsesRuntimeData(statePath string, target appopenclaw.Target) bool {
	return hasRuntimeState(filepath.Join(statePath, "agents"), target)
}

type sessionSnapshot struct {
	Rows     []map[string]any
	Total    int
	Complete bool
}

func collectRuntimeSessions(ctx context.Context, client appopenclaw.Client, loc *time.Location, aliases map[string]string) (sessionSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	snapshot := sessionSnapshot{Rows: make([]map[string]any, 0)}
	seen := map[string]bool{}
	offset := 0
	for page := 0; page < runtimeMaxRows/runtimePageSize; page++ {
		var response struct {
			Sessions   []map[string]any `json:"sessions"`
			TotalCount int              `json:"totalCount"`
			HasMore    bool             `json:"hasMore"`
			NextOffset int              `json:"nextOffset"`
		}
		if err := client.Read(ctx, "sessions.list", map[string]any{"limit": runtimePageSize, "offset": offset}, &response); err != nil {
			return snapshot, err
		}
		if response.Sessions == nil {
			return snapshot, fmt.Errorf("sessions response missing rows")
		}
		for _, s := range response.Sessions {
			key := jsonStr(s, "key")
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			snapshot.Rows = append(snapshot.Rows, runtimeSessionRow(s, loc, aliases))
		}
		snapshot.Total = max(response.TotalCount, len(snapshot.Rows))
		if !response.HasMore {
			snapshot.Complete = true
			return snapshot, nil
		}
		if response.NextOffset <= offset {
			return snapshot, fmt.Errorf("sessions pagination did not advance")
		}
		offset = response.NextOffset
	}
	return snapshot, nil
}

func runtimeSessionRow(s map[string]any, loc *time.Location, aliases map[string]string) map[string]any {
	key := jsonStr(s, "key")
	name := jsonStr(s, "displayName")
	if name == "" {
		name = jsonStr(s, "label")
	}
	if name == "" {
		name = key
	}
	model := jsonStr(s, "model")
	provider := jsonStr(s, "modelProvider")
	if provider != "" && model != "" && !strings.Contains(model, "/") {
		model = provider + "/" + model
	}
	updated, _ := s["updatedAt"].(float64)
	var last string
	if updated > 0 {
		last = time.UnixMilli(int64(updated)).In(loc).Format("15:04:05")
	}
	var pct, total any
	tokens, known := s["totalTokens"].(float64)
	fresh, hasFresh := s["totalTokensFresh"].(bool)
	usageState, contextState := "not_reported", "tokens_not_reported"
	switch {
	case known && (tokens < 0 || math.IsNaN(tokens) || math.IsInf(tokens, 0)):
		usageState, contextState = "invalid", "tokens_invalid"
	case hasFresh && !fresh:
		usageState, contextState = "stale", "tokens_stale"
	case known:
		usageState, contextState = "ready", "limit_not_reported"
		total = tokens
		if contextTokens, ok := s["contextTokens"].(float64); ok && contextTokens > 0 && !math.IsInf(contextTokens, 0) {
			pct = min(100.0, math.Round(tokens/contextTokens*1000)/10)
			contextState = "ready"
		}
	}
	row := map[string]any{
		"key": key, "sessionId": s["sessionId"], "agent": s["agentId"], "name": truncateRunes(name, 100),
		"model": ModelName(aliasOrID(aliases, model)), "modelId": model, "type": s["kind"],
		"contextPct": pct, "contextTokens": s["contextTokens"], "totalTokens": total,
		"tokenState": usageState, "contextState": contextState,
		"updatedAt": updated, "lastActivity": last, "active": s["hasActiveRun"],
		"visibility": s["visibility"], "spawnedBy": s["spawnedBy"], "label": s["label"],
		"runtime": s["agentRuntime"], "isBackground": s["isBackground"], "archived": s["archived"],
	}
	if ids, ok := s["activeRunIds"]; ok {
		row["activeRunIds"] = ids
	}
	if goal := asObj(s["goal"]); goal != nil {
		row["goal"] = projectFields(goal, "objective", "status", "tokensUsed", "tokenBudget", "continuationTurns")
	}
	if placement := asObj(s["placement"]); placement != nil {
		row["placement"] = projectFields(placement, "kind", "state", "status", "nodeId", "deviceId", "host", "label", "runtime", "reason")
	}
	return row
}

func projectFields(source map[string]any, keys ...string) map[string]any {
	result := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := source[key]; ok {
			result[key] = value
		}
	}
	return result
}

func collectRuntimeTasks(ctx context.Context, client appopenclaw.Client, loc *time.Location) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows := make([]map[string]any, 0)
	seenIDs := map[string]bool{}
	seenCursors := map[string]bool{}
	params := map[string]any{"limit": runtimePageSize}
	for page := 0; page < runtimeMaxRows/runtimePageSize; page++ {
		var response struct {
			Tasks      []map[string]any `json:"tasks"`
			NextCursor string           `json:"nextCursor"`
		}
		if err := client.Read(ctx, "tasks.list", params, &response); err != nil {
			return rows, err
		}
		if response.Tasks == nil {
			return rows, fmt.Errorf("tasks response missing rows")
		}
		for _, task := range response.Tasks {
			id := jsonStr(task, "id")
			if id == "" {
				id = jsonStr(task, "taskId")
			}
			if id == "" || seenIDs[id] {
				continue
			}
			seenIDs[id] = true
			row := subagentTaskToRun(task, loc)
			if ended, _ := task["endedAt"].(float64); ended <= 0 {
				row["durationSec"] = nil
			}
			for k, v := range projectFields(task, "runtime", "kind", "sessionKey", "childSessionKey", "ownerKey", "runId", "createdAt", "startedAt", "endedAt", "progressSummary", "terminalSummary", "deliveryStatus", "terminalOutcome") {
				row[k] = v
			}
			row["id"] = id
			row["rawStatus"] = task["status"]
			for _, field := range []string{"task", "error", "progressSummary", "terminalSummary"} {
				if value, ok := row[field].(string); ok {
					row[field] = appopenclaw.Redact(truncateRunes(value, 2000))
				}
			}
			if task["status"] == "completed" {
				row["status"] = jsonStrDefault(task, "terminalOutcome", "succeeded")
			}
			rows = append(rows, row)
		}
		if response.NextCursor == "" {
			return rows, nil
		}
		if seenCursors[response.NextCursor] {
			return rows, fmt.Errorf("tasks pagination did not advance")
		}
		seenCursors[response.NextCursor] = true
		params["cursor"] = response.NextCursor
	}
	return rows, fmt.Errorf("tasks collection reached row limit")
}
