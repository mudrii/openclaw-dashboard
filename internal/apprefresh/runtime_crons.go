package apprefresh

import (
	"context"
	"fmt"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func collectRuntimeCrons(ctx context.Context, client appopenclaw.Client, loc *time.Location) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rows := []map[string]any{}
	seen := map[string]bool{}
	offset := 0
	for page := 0; page < runtimeMaxRows/runtimePageSize; page++ {
		var response struct {
			Jobs       []map[string]any `json:"jobs"`
			HasMore    bool             `json:"hasMore"`
			NextOffset int              `json:"nextOffset"`
		}
		if err := client.Read(ctx, "cron.list", map[string]any{"includeDisabled": true, "limit": runtimePageSize, "offset": offset}, &response); err != nil {
			return rows, err
		}
		if response.Jobs == nil {
			return rows, fmt.Errorf("missing automation inventory")
		}
		for _, job := range response.Jobs {
			id := jsonStr(job, "id")
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			rows = append(rows, cronJobToMap(job, nil, loc))
		}
		if !response.HasMore {
			return rows, nil
		}
		if response.NextOffset <= offset {
			return rows, fmt.Errorf("automation pagination did not advance")
		}
		offset = response.NextOffset
	}
	return rows, fmt.Errorf("automation inventory reached row limit")
}
