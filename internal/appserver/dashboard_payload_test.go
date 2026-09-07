package appserver

import (
	"encoding/json"
	"fmt"
	"testing"
)

// buildDashboardPayload returns a synthetic dashboard payload that mirrors the
// shape of a production data.json: the same 37 top-level keys, the same value
// kinds (nested objects, arrays of objects, strings, numbers, nulls) and a
// comparable serialized size (~38 KB once indented, against ~39 KB in the
// field). Values are fabricated so no live bot data is committed to the repo.
func buildDashboardPayload() map[string]any {
	rows := func(n int, build func(i int) map[string]any) []any {
		out := make([]any, 0, n)
		for i := range n {
			out = append(out, build(i))
		}
		return out
	}

	usage := func(n int) []any {
		return rows(n, func(i int) map[string]any {
			return map[string]any{
				"model":        fmt.Sprintf("provider-%d/model-%d", i%4, i),
				"inputTokens":  float64(1000 + i*37),
				"outputTokens": float64(500 + i*11),
				"cacheRead":    float64(i * 13),
				"cacheWrite":   float64(i * 7),
				"cost":         float64(i) * 0.0125,
				"sessions":     float64(i % 9),
				"lastUsed":     fmt.Sprintf("2026-09-%02dT12:%02d:00Z", 1+i%28, i%60),
				"note":         nil,
			}
		})
	}

	return map[string]any{
		"agentConfig": map[string]any{
			"agents": rows(12, func(i int) map[string]any {
				return map[string]any{
					"name":        fmt.Sprintf("agent-%02d", i),
					"model":       fmt.Sprintf("provider-%d/model-%d", i%3, i),
					"channels":    []any{"telegram", "cli"},
					"enabled":     i%2 == 0,
					"workdir":     fmt.Sprintf("/srv/agents/agent-%02d", i),
					"description": "synthetic agent used for benchmark and decode fixtures",
				}
			}),
			"defaultAgent": "agent-00",
			"gatewayPort":  float64(18789),
		},
		"alerts": rows(6, func(i int) map[string]any {
			return map[string]any{
				"level":   []string{"warn", "critical"}[i%2],
				"code":    fmt.Sprintf("alert_code_%d", i),
				"message": "synthetic alert message describing a degraded collector",
				"since":   "2026-09-05T09:07:00Z",
			}
		}),
		"availableModels": rows(24, func(i int) map[string]any {
			return map[string]any{
				"id":       fmt.Sprintf("provider-%d/model-%d", i%5, i),
				"name":     fmt.Sprintf("Model %d", i),
				"provider": fmt.Sprintf("provider-%d", i%5),
				"context":  float64(200000),
				"pricing":  map[string]any{"input": 0.003, "output": 0.015, "cacheRead": 0.0003},
			}
		}),
		"botEmoji":       "*",
		"botName":        "synthetic-bot-name",
		"compactionMode": "automatic",
		"costBreakdown": rows(10, func(i int) map[string]any {
			return map[string]any{
				"model": fmt.Sprintf("provider-%d/model-%d", i%4, i),
				"cost":  float64(i) * 1.75,
				"share": float64(i) / 10,
			}
		}),
		"costBreakdownToday": rows(1, func(i int) map[string]any {
			return map[string]any{"model": "provider-0/model-0", "cost": 1.25, "share": 1.0}
		}),
		"crons": rows(7, func(i int) map[string]any {
			return map[string]any{
				"id":       fmt.Sprintf("cron-%d", i),
				"schedule": "0 */4 * * *",
				"agent":    fmt.Sprintf("agent-%02d", i%12),
				"lastRun":  "2026-09-05T04:00:00Z",
				"nextRun":  "2026-09-05T08:00:00Z",
				"enabled":  true,
				"summary":  "synthetic scheduled job summary text for fixture padding",
			}
		}),
		"dailyChart": rows(30, func(i int) map[string]any {
			return map[string]any{
				"date":         fmt.Sprintf("2026-08-%02d", 1+i%28),
				"cost":         float64(i) * 0.42,
				"inputTokens":  float64(12000 + i*310),
				"outputTokens": float64(4000 + i*90),
				"cacheRead":    float64(i * 900),
				"cacheWrite":   float64(i * 120),
				"sessions":     float64(i % 11),
			}
		}),
		"gateway": map[string]any{
			"status":       "online",
			"statusReason": nil,
			"port":         float64(18789),
			"pid":          "12345",
			"uptime":       "3d 4h",
			"rssBytes":     float64(184549376),
		},
		"gitLog": rows(5, func(i int) map[string]any {
			return map[string]any{
				"hash":    fmt.Sprintf("%07x", 0xabcdef0+i),
				"subject": "synthetic commit subject line used only as fixture padding",
				"date":    "2026-09-04T18:22:10Z",
				"author":  "Fixture Author",
			}
		}),
		"lastRefresh":   "2026-09-05T09:07:00.000Z",
		"lastRefreshMs": float64(1441),
		"logConfig": map[string]any{
			"source":    "file",
			"path":      "/var/log/openclaw/gateway.log",
			"unit":      nil,
			"maxLines":  float64(500),
			"available": true,
			"profile":   "default",
		},
		"projectedMonthly": 128.75,
		"sessionCount":     float64(48),
		"sessions": rows(1, func(i int) map[string]any {
			return map[string]any{
				"key":       "agent:main:main",
				"agent":     "agent-00",
				"model":     "provider-0/model-0",
				"messages":  float64(212),
				"updatedAt": "2026-09-05T09:00:00Z",
			}
		}),
		"skills": rows(41, func(i int) map[string]any {
			return map[string]any{
				"name":        fmt.Sprintf("skill-%02d", i),
				"description": "synthetic skill description, padded to the length of a real one so the fixture lands near the size of a production data.json rather than degenerating into a syscall benchmark",
				"source":      "plugin",
				"enabled":     i%3 != 0,
			}
		}),
		"subagentCost30d":     float64(0),
		"subagentCost7d":      float64(0),
		"subagentCostAllTime": float64(0),
		"subagentCostToday":   float64(0),
		"subagentRuns":        []any{},
		"subagentRuns30d":     []any{},
		"subagentRuns7d":      []any{},
		"subagentRunsToday":   []any{},
		"subagentUsage":       []any{},
		"subagentUsage30d":    []any{},
		"subagentUsage7d":     []any{},
		"subagentUsageToday":  []any{},
		"tokenUsage":          usage(12),
		"tokenUsage30d":       usage(5),
		"tokenUsage7d":        usage(1),
		"tokenUsageToday":     usage(1),
		"totalCostAllTime":    412.9931,
		"totalCostToday":      3.1416,
	}
}

// marshalDashboardPayload serializes the synthetic payload exactly the way the
// production collector writes data.json (apprefresh.RunRefreshCollector uses
// encoding/json v1 MarshalIndent with a two-space indent).
func marshalDashboardPayload(tb testing.TB) []byte {
	tb.Helper()
	out, err := json.MarshalIndent(buildDashboardPayload(), "", "  ")
	if err != nil {
		tb.Fatalf("marshal dashboard payload: %v", err)
	}
	return out
}
