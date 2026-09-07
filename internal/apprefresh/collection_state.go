package apprefresh

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// maxStaleAge caps how long a retained snapshot may keep standing in for a
// failing collection. Past it the payload reports the outage instead of a value
// nobody should still act on.
const maxStaleAge = 24 * time.Hour

var collectionFields = map[string][]string{
	"configuration":   {"agentConfig", "subagentConfig", "skills", "availableModels", "compactionMode"},
	"sessions":        {"sessions", "sessionCount", "sessionTotal"},
	"crons":           {"crons"},
	"tasks":           {"tasks", "subagentRuns", "subagentRunsToday", "subagentRuns7d", "subagentRuns30d"},
	"usageAll":        {"usageAll", "tokenUsage", "totalCostAllTime", "costBreakdown"},
	"usageToday":      {"usageToday", "tokenUsageToday", "totalCostToday", "projectedMonthly", "costBreakdownToday"},
	"usage7d":         {"usage7d", "tokenUsage7d"},
	"usage30d":        {"usage30d", "tokenUsage30d", "dailyChart"},
	"memory":          {"memory"},
	"memoryIndex":     {"memoryIndex"},
	"skillInventory":  {"skillInventory"},
	"pluginInventory": {"pluginInventory"},
	"channels":        {"channels"},
	"runtimeHealth":   {"runtimeHealth"},
	"gatewayHealth":   {"gatewayHealth"},
	"providerUsage":   {"providerUsage"},
	"runtimeInfo":     {"runtimeInfo"},
	"diagnostics":     {"diagnostics"},
	"modelReadiness":  {"modelReadiness"},
}

func readPreviousSnapshot(path string) map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var previous map[string]any
	if json.NewDecoder(io.LimitReader(f, appopenclaw.MaxOutputBytes)).Decode(&previous) != nil {
		return nil
	}
	return previous
}

func retainLastGoodCollections(current, previous map[string]any) {
	if current["stateDir"] != previous["stateDir"] || current["timezone"] != previous["timezone"] {
		return
	}
	var oldTarget, newTarget appopenclaw.Target
	oldJSON, _ := json.Marshal(previous["runtimeTarget"])
	newJSON, _ := json.Marshal(current["runtimeTarget"])
	if json.Unmarshal(oldJSON, &oldTarget) != nil || json.Unmarshal(newJSON, &newTarget) != nil || oldTarget != newTarget {
		return
	}
	statuses, ok := current["collections"].(map[string]CollectionStatus)
	if !ok {
		return
	}
	oldStatuses := asObj(previous["collections"])
	for name, fields := range collectionFields {
		status, exists := statuses[name]
		warming := strings.HasPrefix(name, "usage") && status.State == "partial"
		if !exists || (status.State != "unavailable" && !warming) {
			continue
		}
		old := asObj(oldStatuses[name])
		if warming && old["complete"] != true && (old["state"] != "stale" || asObj(previous[name])["tokensComplete"] != true) {
			continue
		}
		collectedAt := jsonStr(old, "collectedAt")
		if jsonStr(old, "source") != status.Source || collectedAt == "" {
			continue
		}
		// An unreadable or expired timestamp cannot vouch for the payload, so
		// the outage stands rather than chaining a stale value forever.
		if stamp, err := time.Parse(time.RFC3339, collectedAt); err != nil || time.Since(stamp) > maxStaleAge {
			continue
		}
		for _, field := range fields {
			if value, exists := previous[field]; exists {
				current[field] = value
			}
		}
		status.State = "stale"
		if warming {
			status.ErrorCode = "refreshing"
		}
		status.CollectedAt = collectedAt
		statuses[name] = status
	}
}
