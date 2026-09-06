package apprefresh

import (
	"context"
	"fmt"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

type RuntimeLogs struct {
	Entries   []LogRecord `json:"entries"`
	Cursor    int64       `json:"cursor"`
	Truncated bool        `json:"truncated"`
	Reset     bool        `json:"reset"`
}

func ReadRuntimeLogs(ctx context.Context, client appopenclaw.Client, limit int) (RuntimeLogs, error) {
	if limit < 1 || limit > 1000 {
		return RuntimeLogs{}, fmt.Errorf("invalid log limit")
	}
	var response struct {
		Lines     []string `json:"lines"`
		Cursor    int64    `json:"cursor"`
		Truncated bool     `json:"truncated"`
		Reset     bool     `json:"reset"`
	}
	if err := client.Read(ctx, "logs.tail", map[string]any{"limit": limit, "maxBytes": 262144}, &response); err != nil {
		return RuntimeLogs{}, err
	}
	if response.Lines == nil {
		return RuntimeLogs{}, fmt.Errorf("missing log lines")
	}
	result := RuntimeLogs{Entries: []LogRecord{}, Cursor: response.Cursor, Truncated: response.Truncated, Reset: response.Reset}
	for _, line := range response.Lines {
		line = appopenclaw.Redact(line)
		record, ok := parseLogLine(line, "gateway.jsonl", time.Time{})
		if !ok {
			continue
		}
		record.Source, record.Line, record.Raw = "gateway", line, line
		result.Entries = append(result.Entries, record)
	}
	return result, nil
}
