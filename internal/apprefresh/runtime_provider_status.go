package apprefresh

import (
	"context"
	"fmt"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// collectRuntimeProviderStatus uses the same selected gateway RPCs as Control
// UI. Provider balances/quotas are separate from session-derived cost totals.
func collectRuntimeProviderStatus(ctx context.Context, client appopenclaw.Client) (map[string]any, map[string]CollectionStatus) {
	data := map[string]any{}
	statuses := map[string]CollectionStatus{}
	var health map[string]any
	err := client.Read(ctx, "health", map[string]any{}, &health)
	if _, ok := health["ok"].(bool); err == nil && !ok {
		err = fmt.Errorf("missing gateway health verdict")
	}
	if err == nil {
		data["gatewayHealth"] = projectFields(health, "ok", "ts")
	}
	statuses["gatewayHealth"] = collectionStatus("gateway.health", err, err == nil)
	var usage struct {
		UpdatedAt  int64            `json:"updatedAt"`
		Refreshing bool             `json:"refreshing"`
		Providers  []map[string]any `json:"providers"`
	}
	err = client.Read(ctx, "usage.status", map[string]any{}, &usage)
	if err == nil && usage.Providers == nil {
		err = fmt.Errorf("missing provider usage")
	}
	if err == nil {
		rows := make([]map[string]any, 0, len(usage.Providers))
		for _, provider := range usage.Providers {
			row := projectFields(provider, "provider", "displayName", "plan", "summary")
			row["hasError"] = jsonStr(provider, "error") != ""
			windows := []map[string]any{}
			for _, window := range jsonArr(provider, "windows") {
				windows = append(windows, projectFields(asObj(window), "label", "usedPercent", "resetAt"))
			}
			billing := []map[string]any{}
			for _, entry := range jsonArr(provider, "billing") {
				billing = append(billing, projectFields(asObj(entry), "type", "label", "amount", "unit"))
			}
			row["windows"], row["billing"] = windows, billing
			rows = append(rows, row)
		}
		data["providerUsage"] = map[string]any{"updatedAt": usage.UpdatedAt, "providers": rows}
	}
	statuses["providerUsage"] = collectionStatus("gateway.usage.status", err, err == nil && !usage.Refreshing)
	return data, statuses
}
