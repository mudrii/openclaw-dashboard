package apprefresh

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// subagentTaskMaxLen bounds the task-description string stored per run so a giant
// prompt cannot bloat data.json.
const subagentTaskMaxLen = 80

// subagentTasksFromBytes extracts the tasks array from `openclaw tasks list
// --json` output: the `{count,runtime,tasks:[…]}` envelope or a bare array.
func subagentTasksFromBytes(data []byte) ([]any, bool) {
	var envelope struct {
		Tasks []any `json:"tasks"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Tasks != nil {
		return envelope.Tasks, true
	}
	var arr []any
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, true
	}
	return nil, false
}

// subagentTaskToRun maps one durable task record to a dashboard subagent-run
// entry. Cost and token fields are intentionally omitted: OpenClaw's tasks store
// does not expose per-run usage after the SQLite migration, and the zero-dep
// dashboard cannot read the SQLite directly.
func subagentTaskToRun(tm map[string]any, loc *time.Location) map[string]any {
	if tm == nil {
		return nil
	}

	createdMs, _ := tm["createdAt"].(float64)
	startedMs, _ := tm["startedAt"].(float64)
	endedMs, _ := tm["endedAt"].(float64)

	durationSec := 0
	if endedMs > 0 {
		base := startedMs
		if base <= 0 {
			base = createdMs
		}
		if base > 0 && endedMs >= base {
			durationSec = int((endedMs - base) / 1000)
		}
	}

	var tsStr, dateStr string
	if createdMs > 0 {
		ct := time.UnixMilli(int64(createdMs)).In(loc)
		tsStr = ct.Format("2006-01-02 15:04")
		dateStr = ct.Format("2006-01-02")
	}

	// Collapse internal whitespace (task prompts are multi-line) so the run
	// renders cleanly on a single table row, then bound the length.
	task := truncateRunes(strings.Join(strings.Fields(jsonStr(tm, "task")), " "), subagentTaskMaxLen)

	return map[string]any{
		"task":        task,
		"agent":       jsonStr(tm, "agentId"),
		"status":      jsonStrDefault(tm, "status", "unknown"),
		"durationSec": durationSec,
		"timestamp":   tsStr,
		"date":        dateStr,
		"error":       jsonStr(tm, "error"),
	}
}

// collectSubagentRuns sources subagent runs from `openclaw tasks list --json
// --runtime subagent`. OpenClaw 2026.6+ moved subagent tracking out of session
// keys (agents/*/sessions/sessions.json) into the durable tasks store, so the
// dashboard's session-key scan no longer finds them. On any CLI failure this
// returns an empty list — the legacy session-key source no longer exists, so an
// empty Sub-Agent panel is the honest result, not a fallback.
func collectSubagentRuns(ctx context.Context, runner func(context.Context, string, ...string) *exec.Cmd, resolve func() string, loc *time.Location) []map[string]any {
	if runner == nil {
		runner = execCommandContext
	}
	if resolve == nil {
		resolve = resolveOpenclawBin
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	out, err := boundedOutput(runner(ctx, resolve(), "tasks", "list", "--json", "--runtime", "subagent"), maxCLIOutputBytes)
	if err != nil {
		return nil
	}
	tasks, ok := subagentTasksFromBytes(out)
	if !ok {
		return nil
	}
	runs := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		if run := subagentTaskToRun(asObj(t), loc); run != nil {
			runs = append(runs, run)
		}
	}
	return runs
}
