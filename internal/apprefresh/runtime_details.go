package apprefresh

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func ReadAutomationRuns(ctx context.Context, client appopenclaw.Client, id string, offset, limit int) (map[string]any, error) {
	if id == "" || len(id) > 512 || strings.ContainsAny(id, `/\\`) || offset < 0 || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid automation history request")
	}
	var response struct {
		Entries    []map[string]any `json:"entries"`
		HasMore    bool             `json:"hasMore"`
		NextOffset int              `json:"nextOffset"`
		Total      int              `json:"total"`
	}
	if err := client.Read(ctx, "cron.runs", map[string]any{"id": id, "offset": offset, "limit": limit}, &response); err != nil {
		return nil, err
	}
	if response.Entries == nil {
		return nil, fmt.Errorf("missing automation run entries")
	}
	rows := make([]map[string]any, 0, len(response.Entries))
	for _, entry := range response.Entries {
		row := projectFields(entry, "jobId", "runAtMs", "ts", "status", "completionStatus", "deliveryStatus", "delivered", "durationMs", "sessionKey", "sessionId", "model", "provider")
		for _, key := range []string{"error", "errorReason", "deliveryError", "deliverySuppressionReason"} {
			if message := jsonStr(entry, key); message != "" {
				row[key] = appopenclaw.Redact(truncateRunes(message, 1000))
			}
		}
		if diagnostic := jsonStr(asObj(entry["diagnostics"]), "summary"); diagnostic != "" {
			row["diagnostics"] = appopenclaw.Redact(truncateRunes(diagnostic, 1000))
		}
		rows = append(rows, row)
	}
	return map[string]any{"entries": rows, "hasMore": response.HasMore, "nextOffset": response.NextOffset, "total": response.Total, "offset": offset, "limit": limit}, nil
}

// ReadSessionWork returns a display-only projection. Widget content, URLs,
// execution grants and bearer view tickets stay in the native control UI.
func ReadSessionWork(ctx context.Context, client appopenclaw.Client, key string) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	result := map[string]any{}
	var progress struct {
		Card map[string]any `json:"card"`
	}
	err := client.Read(ctx, "progressCard.get", map[string]any{"sessionKey": key}, &progress)
	result["progressStatus"] = collectionStatus("gateway.progressCard.get", err, err == nil)
	if progress.Card != nil {
		card := projectFields(progress.Card, "revision", "updatedAt")
		card["markdown"] = appopenclaw.Redact(truncateRunes(jsonStr(progress.Card, "markdown"), 8192))
		steps := []map[string]any{}
		for _, step := range jsonArr(progress.Card, "steps") {
			if len(steps) == 50 {
				break
			}
			s := asObj(step)
			steps = append(steps, map[string]any{"step": appopenclaw.Redact(truncateRunes(jsonStr(s, "step"), 512)), "status": s["status"]})
		}
		card["steps"] = steps
		result["progress"] = card
	}
	var board map[string]any
	err = client.Read(ctx, "board.get", map[string]any{"sessionKey": key}, &board)
	if err == nil && board["widgets"] == nil {
		err = fmt.Errorf("missing board widgets")
	}
	result["boardStatus"] = collectionStatus("gateway.board.get", err, err == nil)
	widgets := []map[string]any{}
	for _, w := range jsonArr(board, "widgets") {
		if len(widgets) == 100 {
			break
		}
		widgets = append(widgets, projectFields(asObj(w), "name", "title", "tabId", "contentKind", "grantState", "revision"))
	}
	result["widgets"] = widgets
	result["revision"] = board["revision"]
	return result
}
