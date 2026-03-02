package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// buildSystemPrompt ports build_dashboard_prompt() from server.py exactly.
func buildSystemPrompt(data map[string]any) string {
	var b strings.Builder

	str := func(m map[string]any, key string) string {
		v, _ := m[key].(string)
		return v
	}
	flt := func(m map[string]any, key string) float64 {
		switch v := m[key].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
		return 0
	}

	lastRefresh, _ := data["lastRefresh"].(string)

	b.WriteString("You are an AI assistant embedded in the OpenClaw Dashboard.\n")
	b.WriteString("Answer questions concisely. Use plain text, no markdown.\n")
	b.WriteString(fmt.Sprintf("Data as of: %s\n", lastRefresh))
	b.WriteString("\n=== GATEWAY ===\n")

	gw, _ := data["gateway"].(map[string]any)
	if gw == nil {
		gw = map[string]any{}
	}
	b.WriteString(fmt.Sprintf("Status: %s | PID: %v | Uptime: %s | Memory: %s\n",
		str(gw, "status"), gw["pid"], str(gw, "uptime"), str(gw, "memory")))

	b.WriteString("\n=== COSTS ===\n")
	b.WriteString(fmt.Sprintf("Today: $%.4f (sub-agents: $%.4f)\n",
		flt(data, "totalCostToday"), flt(data, "subagentCostToday")))
	b.WriteString(fmt.Sprintf("All-time: $%.2f | Projected monthly: $%.0f\n",
		flt(data, "totalCostAllTime"), flt(data, "projectedMonthly")))

	if bd, ok := data["costBreakdown"].([]any); ok && len(bd) > 0 {
		b.WriteString("By model (all-time): ")
		limit := 5
		if len(bd) < limit {
			limit = len(bd)
		}
		for i, item := range bd[:limit] {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			if i > 0 {
				b.WriteString(", ")
			}
			model, _ := m["model"].(string)
			cost := flt(m, "cost")
			b.WriteString(fmt.Sprintf("%s $%.2f", model, cost))
		}
		b.WriteString("\n")
	}

	sessions, _ := data["sessions"].([]any)
	sessionCount, _ := data["sessionCount"].(float64)
	if sessionCount == 0 {
		sessionCount = float64(len(sessions))
	}
	b.WriteString(fmt.Sprintf("\n=== SESSIONS (%.0f total, showing top 3) ===\n", sessionCount))
	top := 3
	if len(sessions) < top {
		top = len(sessions)
	}
	for _, item := range sessions[:top] {
		s, _ := item.(map[string]any)
		if s == nil {
			continue
		}
		ctxPct := flt(s, "contextPct")
		b.WriteString(fmt.Sprintf("  %s | %s | %s | context: %.1f%%\n",
			str(s, "name"), str(s, "model"), str(s, "type"), ctxPct))
	}

	crons, _ := data["crons"].([]any)
	failed := 0
	for _, item := range crons {
		c, _ := item.(map[string]any)
		if c != nil && str(c, "lastStatus") == "error" {
			failed++
		}
	}
	b.WriteString(fmt.Sprintf("\n=== CRON JOBS (%d total, %d failed) ===\n", len(crons), failed))
	cronTop := 5
	if len(crons) < cronTop {
		cronTop = len(crons)
	}
	for _, item := range crons[:cronTop] {
		c, _ := item.(map[string]any)
		if c == nil {
			continue
		}
		status := str(c, "lastStatus")
		errSuffix := ""
		if status == "error" {
			errSuffix = fmt.Sprintf(" ERROR: %s", str(c, "lastError"))
		}
		b.WriteString(fmt.Sprintf("  %s | %s | %s%s\n",
			str(c, "name"), str(c, "schedule"), status, errSuffix))
	}

	b.WriteString("\n=== ALERTS ===\n")
	alerts, _ := data["alerts"].([]any)
	if len(alerts) == 0 {
		b.WriteString("  None\n")
	} else {
		for _, item := range alerts {
			a, _ := item.(map[string]any)
			if a == nil {
				continue
			}
			sev := strings.ToUpper(str(a, "severity"))
			b.WriteString(fmt.Sprintf("  [%s] %s\n", sev, str(a, "message")))
		}
	}

	b.WriteString("\n=== CONFIGURATION ===\n")
	ac, _ := data["agentConfig"].(map[string]any)
	if ac == nil {
		ac = map[string]any{}
	}
	primary := str(ac, "primaryModel")
	b.WriteString(fmt.Sprintf("Primary model: %s\n", primary))
	fallbacks := ""
	if fb, ok := ac["fallbacks"].([]any); ok {
		parts := make([]string, 0, len(fb))
		for _, f := range fb {
			s, _ := f.(string)
			if s != "" {
				parts = append(parts, s)
			}
		}
		fallbacks = strings.Join(parts, ", ")
	}
	if fallbacks == "" {
		fallbacks = "none"
	}
	b.WriteString(fmt.Sprintf("Fallbacks: %s\n", fallbacks))

	return b.String()
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Question string        `json:"question"`
	History  []chatMessage `json:"history"`
}

type completionPayload struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
}

func callGateway(system string, history []chatMessage, question string, port int, token, model string, client *http.Client) (string, error) {
	messages := []chatMessage{{Role: "system", Content: system}}
	messages = append(messages, history...)
	messages = append(messages, chatMessage{Role: "user", Content: question})

	payload := completionPayload{
		Model:     model,
		Messages:  messages,
		MaxTokens: 512,
		Stream:    false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%d/v1/chat/completions", port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		preview := string(respBody)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return "", fmt.Errorf("gateway HTTP %d: %s", resp.StatusCode, preview)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	if len(result.Choices) == 0 {
		return "(empty response)", nil
	}
	content := result.Choices[0].Message.Content
	if content == "" {
		content = "(empty response)"
	}
	return content, nil
}
