package apprefresh

import (
	"context"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// HasExactActiveRun fails closed on absent identities or a truncated listing.
func HasExactActiveRun(ctx context.Context, client appopenclaw.Client, sessionKey, runID string) (bool, error) {
	snapshot, err := collectRuntimeSessions(ctx, client, time.UTC, nil)
	if err != nil {
		return false, err
	}
	for _, row := range snapshot.Rows {
		if row["key"] != sessionKey || row["active"] != true {
			continue
		}
		ids, _ := row["activeRunIds"].([]any)
		for _, id := range ids {
			if id == runID {
				return true, nil
			}
		}
	}
	return false, nil
}
