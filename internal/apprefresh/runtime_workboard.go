package apprefresh

import (
	"context"
	"fmt"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// ReadWorkboardSummary is read-only and intentionally excludes card content,
// workspace configuration, claim tokens and orchestration credentials.
func ReadWorkboardSummary(ctx context.Context, client appopenclaw.Client) (map[string]any, error) {
	var inventory struct {
		Plugins []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"plugins"`
	}
	if err := client.ReadCommand(ctx, "plugins", &inventory); err != nil {
		return nil, err
	}
	loaded := false
	for _, plugin := range inventory.Plugins {
		if plugin.ID == "workboard" && plugin.Status == "loaded" {
			loaded = true
		}
	}
	if !loaded {
		return map[string]any{"state": "disabled_or_unavailable", "boards": []any{}}, nil
	}
	var response struct {
		Boards []map[string]any `json:"boards"`
	}
	if err := client.Read(ctx, "workboard.boards.list", map[string]any{}, &response); err != nil {
		return nil, err
	}
	if response.Boards == nil {
		return nil, fmt.Errorf("missing workboard boards")
	}
	rows := make([]map[string]any, 0, min(len(response.Boards), 100))
	for _, board := range response.Boards[:min(len(response.Boards), 100)] {
		rows = append(rows, projectFields(board, "id", "name", "total", "active", "archived", "byStatus", "updatedAt"))
	}
	return map[string]any{"state": "ready", "boards": rows, "truncated": len(response.Boards) > 100}, nil
}
