package appchat

import (
	"strings"
	"testing"
)

// TestBuildSystemPrompt_RichData exercises the data-rich branches that the
// existing WithData/EmptyData tests never reach: costBreakdown truncation at 5
// entries, non-empty alerts rendering, a failed cron producing the failed count
// plus ERROR line, and joined config fallbacks.
func TestBuildSystemPrompt_RichData(t *testing.T) {
	costBreakdown := make([]any, 0, 7)
	for _, m := range []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7"} {
		costBreakdown = append(costBreakdown, map[string]any{"model": m, "cost": 1.5})
	}

	data := map[string]any{
		"lastRefresh":   "2026-01-01T00:00:00Z",
		"costBreakdown": costBreakdown,
		"crons": []any{
			map[string]any{
				"name":            "broken-job",
				"schedule":        "* * * * *",
				"lastStatus":      "error",
				"lastDiagnostics": []any{"boom"}, // real collector field (string array)
			},
			map[string]any{
				"name":       "good-job",
				"schedule":   "0 0 * * *",
				"lastStatus": "ok",
			},
		},
		"alerts": []any{
			map[string]any{"severity": "high", "message": "cost spike"},
		},
		"agentConfig": map[string]any{
			"primaryModel": "opus-4",
			"fallbacks":    []any{"sonnet", "haiku"},
		},
	}

	prompt := BuildSystemPrompt(data)

	// costBreakdown truncated at 5: m5 present, m6/m7 absent.
	for _, want := range []string{"m1 $1.50", "m5 $1.50"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing costBreakdown entry %q", want)
		}
	}
	for _, notWant := range []string{"m6 $", "m7 $"} {
		if strings.Contains(prompt, notWant) {
			t.Errorf("prompt should truncate costBreakdown at 5, found %q", notWant)
		}
	}

	// Cron header reports 1 failed and the failed job renders its ERROR line.
	if !strings.Contains(prompt, "1 failed") {
		t.Errorf("prompt missing failed cron count; got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "ERROR: boom") {
		t.Errorf("prompt missing cron ERROR line; got:\n%s", prompt)
	}

	// Alerts rendered with uppercased severity and message.
	if !strings.Contains(prompt, "[HIGH] cost spike") {
		t.Errorf("prompt missing rendered alert; got:\n%s", prompt)
	}
	if strings.Contains(prompt, "None") {
		t.Errorf("prompt should not render 'None' when alerts are present")
	}

	// Fallbacks joined with ", ".
	if !strings.Contains(prompt, "Fallbacks: sonnet, haiku") {
		t.Errorf("prompt missing joined fallbacks; got:\n%s", prompt)
	}
}
