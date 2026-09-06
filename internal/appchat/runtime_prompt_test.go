package appchat

import (
	"strings"
	"testing"
)

func TestRuntimePromptDoesNotInventZeroCosts(t *testing.T) {
	prompt := BuildSystemPrompt(map[string]any{"schemaVersion": float64(2), "totalCostToday": nil, "totalCostAllTime": nil, "projectedMonthly": nil, "subagentCostToday": nil, "sessions": []any{map[string]any{"contextPct": nil}}})
	if strings.Contains(prompt, "Today: $0") || strings.Contains(prompt, "All-time: $0") {
		t.Fatalf("unknown costs became zero: %s", prompt)
	}
	if !strings.Contains(prompt, "Unknown") {
		t.Fatal("missing unknown cost explanation")
	}
	if !strings.Contains(prompt, "context: Unknown") {
		t.Fatal("unknown context became zero")
	}
}
