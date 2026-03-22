package dashboard

import (
	"time"

	apprefresh "github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

func bucketsToList(m map[string]*tokenBucket) []tokenUsageEntry {
	return apprefresh.BucketsToList(m)
}

func fmtTokens(n int) string {
	return apprefresh.FmtTokens(n)
}

func buildSIDToKeyMap(stores []sessionStoreFile) map[string]string {
	return apprefresh.BuildSIDToKeyMap(stores)
}

func collectTokenUsage(
	basePath string, loc *time.Location,
	todayStr, date7d, date30d string,
	knownSIDs map[string]string, sidToKey map[string]string,
	modelAliases map[string]string,
	modelsAll, modelsToday, models7d, models30d map[string]*tokenBucket,
	subagentAll, subagentToday, subagent7d, subagent30d map[string]*tokenBucket,
	dailyCosts map[string]map[string]float64,
	dailyTokens map[string]map[string]int,
	dailyCalls map[string]map[string]int,
	dailySubagentCosts map[string]float64,
	dailySubagentCount map[string]int,
) []map[string]any {
	return apprefresh.CollectTokenUsage(
		basePath, loc, todayStr, date7d, date30d,
		knownSIDs, sidToKey, modelAliases,
		modelsAll, modelsToday, models7d, models30d,
		subagentAll, subagentToday, subagent7d, subagent30d,
		dailyCosts, dailyTokens, dailyCalls, dailySubagentCosts, dailySubagentCount,
	)
}
