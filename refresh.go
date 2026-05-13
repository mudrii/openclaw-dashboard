package dashboard

import (
	"context"
	"time"

	apprefresh "github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

type tokenBucket = apprefresh.TokenBucket
type tokenUsageEntry = apprefresh.TokenUsageEntry
type sessionStoreFile = apprefresh.SessionStoreFile

var refreshCollectorFunc = runRefreshCollectorWithContext

func runRefreshCollectorWithContext(ctx context.Context, dashboardDir, openclawPath string, cfg Config) error {
	return apprefresh.RunRefreshCollector(ctx, dashboardDir, openclawPath, cfg)
}

func titleCase(s string) string {
	return apprefresh.TitleCase(s)
}

func trimLabel(s string) string {
	return apprefresh.TrimLabel(s)
}

func modelName(model string) string {
	return apprefresh.ModelName(model)
}

func collectCrons(cronPath string, loc *time.Location) []map[string]any {
	return apprefresh.CollectCrons(cronPath, loc)
}

func buildDailyChart(now time.Time, dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int, dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64, dailySubagentCount map[string]int,
	dashboardDir string) []map[string]any {
	return apprefresh.BuildDailyChart(now, dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount, dashboardDir)
}

func buildAlerts(totalCostToday, costHigh, costWarn float64,
	crons []map[string]any, sessions []map[string]any,
	contextThreshold float64, gateway map[string]any, memThresholdKB float64) []map[string]any {
	return apprefresh.BuildAlerts(totalCostToday, costHigh, costWarn, crons, sessions, contextThreshold, gateway, memThresholdKB)
}

func buildCostBreakdown(m map[string]*tokenBucket) []map[string]any {
	return apprefresh.BuildCostBreakdown(m)
}

func filterByDate(runs []map[string]any, targetDate, op string) []map[string]any {
	return apprefresh.FilterByDate(runs, targetDate, op)
}

func limitSlice[T any](s []T, max int) []T {
	return apprefresh.LimitSlice(s, max)
}
