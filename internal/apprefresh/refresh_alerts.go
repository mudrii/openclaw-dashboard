package apprefresh

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Status string constants used to classify cron and gateway state in alerts.
// Compared case-insensitively so upstream variations ("Error", "OFFLINE") still trigger alerts.
const (
	statusError   = "error"
	statusOffline = "offline"
)

// BuildAlerts produces user-visible alert entries from aggregated state. Order
// is deterministic: cost → cron failures → high context sessions → gateway
// offline → high memory. Each entry has type/icon/message/severity.
func BuildAlerts(totalCostToday, costHigh, costWarn float64,
	crons []map[string]any, sessions []map[string]any,
	contextThreshold float64, gateway map[string]any, memThresholdKB float64) []map[string]any {

	var alerts []map[string]any

	if totalCostToday > costHigh {
		alerts = append(alerts, map[string]any{
			"type": "warning", "icon": "💰",
			"message":  fmt.Sprintf("High daily cost: $%.2f", totalCostToday),
			"severity": "high",
		})
	} else if totalCostToday > costWarn {
		alerts = append(alerts, map[string]any{
			"type": "info", "icon": "💵",
			"message":  fmt.Sprintf("Daily cost above $%.0f: $%.2f", costWarn, totalCostToday),
			"severity": "medium",
		})
	}

	for _, c := range crons {
		s, _ := c["lastStatus"].(string)
		if strings.EqualFold(s, statusError) {
			name, _ := c["name"].(string)
			alerts = append(alerts, map[string]any{
				"type": "error", "icon": "❌",
				"message":  "Cron failed: " + name,
				"severity": "high",
			})
		}
	}

	for _, s := range sessions {
		pct, _ := s["contextPct"].(float64)
		if pct > contextThreshold {
			name, _ := s["name"].(string)
			if len(name) > 30 {
				name = name[:30]
			}
			alerts = append(alerts, map[string]any{
				"type": "warning", "icon": "⚠️",
				"message":  fmt.Sprintf("High context: %s (%.0f%%)", name, pct),
				"severity": "medium",
			})
		}
	}

	if gwStatus, _ := gateway["status"].(string); strings.EqualFold(gwStatus, statusOffline) {
		alerts = append(alerts, map[string]any{
			"type": "error", "icon": "🔴",
			"message": "Gateway is offline", "severity": "critical",
		})
	}

	rss, _ := gateway["rss"].(int)
	if rss > int(memThresholdKB) {
		mem, _ := gateway["memory"].(string)
		alerts = append(alerts, map[string]any{
			"type": "warning", "icon": "🧠",
			"message":  "High memory usage: " + mem,
			"severity": "medium",
		})
	}

	return alerts
}

// BuildCostBreakdown returns model→cost entries sorted by cost descending,
// rounded to cents, dropping zero-cost models.
func BuildCostBreakdown(m map[string]*TokenBucket) []map[string]any {
	type kv struct {
		model string
		cost  float64
	}
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		if v.Cost > 0 {
			pairs = append(pairs, kv{k, v.Cost})
		}
	}
	slices.SortFunc(pairs, func(a, b kv) int { return cmp.Compare(b.cost, a.cost) })
	var out []map[string]any
	for _, p := range pairs {
		out = append(out, map[string]any{
			"model": p.model,
			"cost":  math.Round(p.cost*100) / 100,
		})
	}
	return out
}
