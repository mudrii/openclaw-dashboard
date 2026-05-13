package apprefresh

import (
	"cmp"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// BuildDailyChart returns 30 calendar days of cost/tokens/calls plus a
// per-model breakdown (top 6 models + "Other" bucket). Frozen-daily.json
// merge contract: see comment inline.
func BuildDailyChart(now time.Time, dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int, dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64, dailySubagentCount map[string]int,
	dashboardDir string) []map[string]any {

	// Generate last 30 days
	var chartDates []string
	for i := 29; i >= 0; i-- {
		chartDates = append(chartDates, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}

	// Find top 6 models by cost
	modelTotals := map[string]float64{}
	for _, d := range chartDates {
		for m, c := range dailyCosts[d] {
			modelTotals[m] += c
		}
	}
	type modelCost struct {
		model string
		cost  float64
	}
	var sorted []modelCost
	for m, c := range modelTotals {
		sorted = append(sorted, modelCost{m, c})
	}
	slices.SortFunc(sorted, func(a, b modelCost) int { return cmp.Compare(b.cost, a.cost) })
	topModels := map[string]bool{}
	for i, mc := range sorted {
		if i >= 6 {
			break
		}
		topModels[mc.model] = true
	}

	var chart []map[string]any
	for _, d := range chartDates {
		dayModels := dailyCosts[d]
		dayTokens := dailyTokens[d]
		dayCalls := dailyCalls[d]

		var totalCost float64
		for _, c := range dayModels {
			totalCost += c
		}
		var totalTokens int
		for _, t := range dayTokens {
			totalTokens += t
		}
		var totalCalls int
		for _, c := range dayCalls {
			totalCalls += c
		}

		models := map[string]any{}
		var otherCost float64
		for m, c := range dayModels {
			if topModels[m] {
				models[m] = math.Round(c*10000) / 10000
			} else {
				otherCost += c
			}
		}
		if otherCost > 0 {
			models["Other"] = math.Round(otherCost*10000) / 10000
		}

		chart = append(chart, map[string]any{
			"date":         d,
			"label":        d[5:],
			"total":        math.Round(totalCost*100) / 100,
			"tokens":       totalTokens,
			"calls":        totalCalls,
			"subagentCost": math.Round(dailySubagentCosts[d]*100) / 100,
			"subagentRuns": dailySubagentCount[d],
			"models":       models,
		})
	}

	// Merge frozen historical data.
	//
	// Contract: a frozen entry overrides the computed chart entry for a given
	// day ONLY when frozen.total > computed.total. When the gate passes, ALL
	// fields from frozen replace the computed entry's matching fields (total,
	// tokens, subagentRuns, subagentCost, models). When the gate fails, the
	// computed entry is preserved wholesale — frozen.tokens/subagent* are
	// ignored even if higher. Rationale: frozen-daily.json is a manually
	// curated backstop for days where the live aggregation has gaps; only
	// when frozen reports a strictly higher cost do we trust its full
	// snapshot for that day. Pinned by TestBuildDailyChart_FrozenMergeContract.
	frozenPath := filepath.Join(dashboardDir, "frozen-daily.json")
	if data, err := os.ReadFile(frozenPath); err == nil {
		var frozen map[string]map[string]any
		if json.Unmarshal(data, &frozen) == nil {
			for i, entry := range chart {
				d, _ := entry["date"].(string)
				if f, ok := frozen[d]; ok {
					fTotal, _ := f["total"].(float64)
					eTotal, _ := entry["total"].(float64)
					if fTotal > eTotal {
						chart[i]["total"] = math.Round(fTotal*100) / 100
						if t, ok := f["tokens"].(float64); ok {
							chart[i]["tokens"] = int(t)
						}
						if sr, ok := f["subagentRuns"].(float64); ok {
							chart[i]["subagentRuns"] = int(sr)
						}
						if sc, ok := f["subagentCost"].(float64); ok {
							chart[i]["subagentCost"] = math.Round(sc*100) / 100
						}
						if m, ok := f["models"].(map[string]any); ok {
							rounded := map[string]any{}
							for k, v := range m {
								if fv, ok := v.(float64); ok {
									rounded[k] = math.Round(fv*10000) / 10000
								}
							}
							chart[i]["models"] = rounded
						}
					}
				}
			}
		}
	}

	return chart
}
