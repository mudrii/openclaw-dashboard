package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Cost formatting helpers for prompt building.
func fmtCost2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
func fmtCost4(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
func fmtCost0(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) }
func fmtPct(v float64) string   { return strconv.FormatFloat(v, 'f', 1, 64) }

// buildSystemPrompt constructs the AI assistant context from dashboard data.
// Optimised: direct WriteString calls instead of fmt.Sprintf to avoid heap allocs.
func buildSystemPrompt(data map[string]any) string {
	var b strings.Builder
	b.Grow(2048) // typical prompt ~1-2KB; avoids ~4 internal re-allocations

	lastRefresh := getStr(data, "lastRefresh")

	b.WriteString("You are an AI assistant embedded in the OpenClaw Dashboard.\n")
	b.WriteString("Answer questions concisely. Use plain text, no markdown.\n")
	b.WriteString("Data as of: ")
	b.WriteString(lastRefresh)
	b.WriteByte('\n')

	appendGatewaySection(&b, data)
	appendCostsSection(&b, data)
	appendSessionsSection(&b, data)
	appendCronsSection(&b, data)
	appendAlertsSection(&b, data)
	appendConfigSection(&b, data)

	return b.String()
}

func appendGatewaySection(b *strings.Builder, data map[string]any) {
	b.WriteString("\n=== GATEWAY ===\n")
	gw := getMap(data, "gateway")
	b.WriteString("Status: ")
	b.WriteString(getStr(gw, "status"))
	b.WriteString(" | PID: ")
	b.WriteString(fmtAny(gw["pid"]))
	b.WriteString(" | Uptime: ")
	b.WriteString(getStr(gw, "uptime"))
	b.WriteString(" | Memory: ")
	b.WriteString(getStr(gw, "memory"))
	b.WriteByte('\n')
}

func appendCostsSection(b *strings.Builder, data map[string]any) {
	b.WriteString("\n=== COSTS ===\n")
	b.WriteString("Today: $")
	b.WriteString(fmtCost4(getFloat(data, "totalCostToday")))
	b.WriteString(" (sub-agents: $")
	b.WriteString(fmtCost4(getFloat(data, "subagentCostToday")))
	b.WriteString(")\n")
	b.WriteString("All-time: $")
	b.WriteString(fmtCost2(getFloat(data, "totalCostAllTime")))
	b.WriteString(" | Projected monthly: $")
	b.WriteString(fmtCost0(getFloat(data, "projectedMonthly")))
	b.WriteByte('\n')

	if bd := getSlice(data, "costBreakdown"); len(bd) > 0 {
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
			b.WriteString(getStr(m, "model"))
			b.WriteString(" $")
			b.WriteString(fmtCost2(getFloat(m, "cost")))
		}
		b.WriteByte('\n')
	}
}

func appendSessionsSection(b *strings.Builder, data map[string]any) {
	sessions := getSlice(data, "sessions")
	sessionCount := getFloat(data, "sessionCount")
	if sessionCount == 0 {
		sessionCount = float64(len(sessions))
	}
	b.WriteString("\n=== SESSIONS (")
	b.WriteString(fmtCost0(sessionCount))
	b.WriteString(" total, showing top 3) ===\n")
	top := 3
	if len(sessions) < top {
		top = len(sessions)
	}
	for _, item := range sessions[:top] {
		s, _ := item.(map[string]any)
		if s == nil {
			continue
		}
		b.WriteString("  ")
		b.WriteString(getStr(s, "name"))
		b.WriteString(" | ")
		b.WriteString(getStr(s, "model"))
		b.WriteString(" | ")
		b.WriteString(getStr(s, "type"))
		b.WriteString(" | context: ")
		b.WriteString(fmtPct(getFloat(s, "contextPct")))
		b.WriteString("%\n")
	}
}

func appendCronsSection(b *strings.Builder, data map[string]any) {
	crons := getSlice(data, "crons")
	failed := 0
	for _, item := range crons {
		c, _ := item.(map[string]any)
		if c != nil && getStr(c, "lastStatus") == "error" {
			failed++
		}
	}
	b.WriteString("\n=== CRON JOBS (")
	b.WriteString(strconv.Itoa(len(crons)))
	b.WriteString(" total, ")
	b.WriteString(strconv.Itoa(failed))
	b.WriteString(" failed) ===\n")
	cronTop := 5
	if len(crons) < cronTop {
		cronTop = len(crons)
	}
	for _, item := range crons[:cronTop] {
		c, _ := item.(map[string]any)
		if c == nil {
			continue
		}
		status := getStr(c, "lastStatus")
		b.WriteString("  ")
		b.WriteString(getStr(c, "name"))
		b.WriteString(" | ")
		b.WriteString(getStr(c, "schedule"))
		b.WriteString(" | ")
		b.WriteString(status)
		if status == "error" {
			b.WriteString(" ERROR: ")
			b.WriteString(getStr(c, "lastError"))
		}
		b.WriteByte('\n')
	}
}

func appendAlertsSection(b *strings.Builder, data map[string]any) {
	b.WriteString("\n=== ALERTS ===\n")
	alerts := getSlice(data, "alerts")
	if len(alerts) == 0 {
		b.WriteString("  None\n")
		return
	}
	for _, item := range alerts {
		a, _ := item.(map[string]any)
		if a == nil {
			continue
		}
		b.WriteString("  [")
		b.WriteString(strings.ToUpper(getStr(a, "severity")))
		b.WriteString("] ")
		b.WriteString(getStr(a, "message"))
		b.WriteByte('\n')
	}
}

func appendConfigSection(b *strings.Builder, data map[string]any) {
	b.WriteString("\n=== CONFIGURATION ===\n")
	ac := getMap(data, "agentConfig")
	b.WriteString("Primary model: ")
	b.WriteString(getStr(ac, "primaryModel"))
	b.WriteByte('\n')
	fallbacks := ""
	if fb := getSlice(ac, "fallbacks"); len(fb) > 0 {
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
	b.WriteString("Fallbacks: ")
	b.WriteString(fallbacks)
	b.WriteByte('\n')
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

func callGateway(ctx context.Context, system string, history []chatMessage, question string, port int, token, model string, maxTokens int, client *http.Client) (string, error) {
	// Pre-allocate messages slice: system + history + user question
	messages := make([]chatMessage, 0, 2+len(history))
	messages = append(messages, chatMessage{Role: "system", Content: system})
	messages = append(messages, history...)
	messages = append(messages, chatMessage{Role: "user", Content: question})

	payload := completionPayload{
		Model:     model,
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal error: %w", err)
	}

	url := "http://localhost:" + strconv.Itoa(port) + gatewayAPIPath
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayResp+1))
	if err != nil {
		return "", fmt.Errorf("read error: %w", err)
	}
	if len(respBody) > maxGatewayResp {
		return "", fmt.Errorf("gateway response too large (>%d bytes)", maxGatewayResp)
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
