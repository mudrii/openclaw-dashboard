package apprefresh

import (
	"context"
	"fmt"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

type runtimeUsageTotals struct {
	Input              int            `json:"input"`
	Output             int            `json:"output"`
	CacheRead          int            `json:"cacheRead"`
	CacheWrite         int            `json:"cacheWrite"`
	TotalTokens        int            `json:"totalTokens"`
	TotalCost          *float64       `json:"totalCost"`
	MissingCostEntries int            `json:"missingCostEntries"`
	MissingCostByModel map[string]int `json:"missingCostByModel"`
}

type runtimeUsage struct {
	Totals     *runtimeUsageTotals `json:"totals"`
	Aggregates struct {
		ByModel []struct {
			Provider string             `json:"provider"`
			Model    string             `json:"model"`
			Count    int                `json:"count"`
			Totals   runtimeUsageTotals `json:"totals"`
		} `json:"byModel"`
		Daily []struct {
			Date     string  `json:"date"`
			Tokens   int     `json:"tokens"`
			Cost     float64 `json:"cost"`
			Messages int     `json:"messages"`
		} `json:"daily"`
		ModelDaily []struct {
			Date     string  `json:"date"`
			Provider string  `json:"provider"`
			Model    string  `json:"model"`
			Cost     float64 `json:"cost"`
		} `json:"modelDaily"`
	} `json:"aggregates"`
	CacheStatus struct {
		Status       string `json:"status"`
		CachedFiles  int    `json:"cachedFiles"`
		PendingFiles int    `json:"pendingFiles"`
		StaleFiles   int    `json:"staleFiles"`
	} `json:"cacheStatus"`
}

func collectRuntimeUsage(ctx context.Context, client appopenclaw.Client, period, start, end, timezone string) (runtimeUsage, error) {
	params := map[string]any{"agentScope": "all", "mode": "specific", "timeZone": timezone, "limit": 1, "includeContextWeight": false}
	if period != "" {
		params["range"] = period
	} else {
		params["startDate"], params["endDate"] = start, end
	}
	var result runtimeUsage
	if err := client.Read(ctx, "sessions.usage", params, &result); err != nil {
		return result, err
	}
	if result.Totals == nil {
		return result, fmt.Errorf("usage response missing totals")
	}
	return result, nil
}

func (u runtimeUsage) CompleteTokens() bool {
	return u.Totals != nil && u.CacheStatus.Status == "fresh" && u.CacheStatus.PendingFiles == 0 && u.CacheStatus.StaleFiles == 0
}

func (u runtimeUsage) CompleteCost() bool {
	return u.CompleteTokens() && u.Totals.TotalCost != nil && u.Totals.MissingCostEntries == 0
}

func (u runtimeUsage) ModelRows() []map[string]any {
	rows := make([]map[string]any, 0, len(u.Aggregates.ByModel))
	for _, model := range u.Aggregates.ByModel {
		t := model.Totals
		id := model.Model
		if model.Provider != "" {
			id = model.Provider + "/" + id
		}
		var cost any
		if u.CompleteTokens() && t.MissingCostEntries == 0 && t.TotalCost != nil {
			cost = round2(*t.TotalCost)
		}
		rows = append(rows, map[string]any{
			"model": ModelName(id), "modelId": id, "calls": model.Count,
			"input": FmtTokens(t.Input), "output": FmtTokens(t.Output), "cacheRead": FmtTokens(t.CacheRead), "cacheWrite": FmtTokens(t.CacheWrite), "totalTokens": FmtTokens(t.TotalTokens),
			"inputRaw": t.Input, "outputRaw": t.Output, "cacheReadRaw": t.CacheRead, "cacheWriteRaw": t.CacheWrite, "totalTokensRaw": t.TotalTokens,
			"cost": cost, "knownCost": t.TotalCost, "costComplete": cost != nil, "tokensComplete": u.CompleteTokens(), "missingCostEntries": t.MissingCostEntries,
		})
	}
	return rows
}

// applyRuntimeUsage uses gateway-wide aggregates, never the limited session page
// or the legacy frozen-daily backstop. A known subtotal is not a total price.
func applyRuntimeUsage(data map[string]any, suffix string, u runtimeUsage, now time.Time) {
	key := "tokenUsage" + suffix
	if suffix == "All" {
		key = "tokenUsage"
	}
	data[key] = u.ModelRows()
	metadata := map[string]any{"timezone": now.Location().String(), "scope": "all", "tokensComplete": u.CompleteTokens(), "costComplete": u.CompleteCost(), "cacheStatus": u.CacheStatus}
	var cost any
	if u.Totals != nil {
		metadata["totalTokens"] = u.Totals.TotalTokens
		metadata["knownCost"] = u.Totals.TotalCost
		metadata["missingCostEntries"] = u.Totals.MissingCostEntries
		metadata["missingCostByModel"] = u.Totals.MissingCostByModel
		if u.CompleteCost() {
			cost = round2(*u.Totals.TotalCost)
		}
	}
	data["usage"+suffix] = metadata
	var breakdown any
	if u.CompleteCost() {
		rows := make([]map[string]any, 0, len(u.Aggregates.ByModel))
		for _, model := range u.Aggregates.ByModel {
			if model.Totals.TotalCost == nil || model.Totals.MissingCostEntries != 0 {
				rows = nil
				break
			}
			id := model.Model
			if model.Provider != "" {
				id = model.Provider + "/" + id
			}
			rows = append(rows, map[string]any{"model": ModelName(id), "cost": *model.Totals.TotalCost})
		}
		if rows != nil {
			breakdown = rows
		}
	}
	if suffix == "All" {
		data["totalCostAllTime"], data["costBreakdown"] = cost, breakdown
	}
	if suffix == "Today" {
		data["totalCostToday"], data["costBreakdownToday"], data["projectedMonthly"] = cost, breakdown, nil
		if cost != nil {
			data["projectedMonthly"] = round2(*u.Totals.TotalCost * 30)
		}
	}
	if suffix == "30d" {
		chart := make([]map[string]any, 0, 30)
		for i := 29; i >= 0; i-- {
			date := now.AddDate(0, 0, -i).Format("2006-01-02")
			row := map[string]any{"date": date, "label": date[5:], "total": nil, "tokens": nil, "calls": nil, "models": map[string]any{}, "costComplete": u.CompleteCost()}
			if u.CompleteTokens() {
				row["tokens"], row["calls"] = 0, 0
			}
			if u.CompleteCost() {
				row["total"] = float64(0)
			}
			for _, daily := range u.Aggregates.Daily {
				if daily.Date == date {
					row["tokens"], row["calls"] = daily.Tokens, daily.Messages
					if u.CompleteCost() {
						row["total"] = round2(daily.Cost)
					}
				}
			}
			if u.CompleteCost() {
				models := row["models"].(map[string]any)
				for _, daily := range u.Aggregates.ModelDaily {
					if daily.Date == date {
						models[ModelName(daily.Provider+"/"+daily.Model)] = daily.Cost
					}
				}
			}
			chart = append(chart, row)
		}
		data["dailyChart"] = chart
	}
}
