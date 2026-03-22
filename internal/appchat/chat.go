package appchat

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

const maxGatewayResp = 1 << 20

// BuildSystemPrompt builds a compact system prompt from dashboard data.
// Optimised: direct WriteString calls instead of fmt.Sprintf to avoid heap allocs.
func BuildSystemPrompt(data map[string]any) string {
	var b strings.Builder
	b.Grow(2048) // typical prompt ~1-2KB; avoids ~4 internal re-allocations

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
	fmtCost2 := func(v float64) string {
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	fmtCost4 := func(v float64) string {
		return strconv.FormatFloat(v, 'f', 4, 64)
	}
	fmtCost0 := func(v float64) string {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	fmtPct := func(v float64) string {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	fmtAny := func(v any) string {
		if v == nil {
			return "<nil>"
		}
		switch t := v.(type) {
		case string:
			return t
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64)
		case int:
			return strconv.Itoa(t)
		default:
			return fmt.Sprint(v)
		}
	}

	lastRefresh, _ := data["lastRefresh"].(string)

	b.WriteString("You are an AI assistant embedded in the OpenClaw Dashboard.\n")
	b.WriteString("Answer questions concisely. Use plain text, no markdown.\n")
	b.WriteString("Data as of: ")
	b.WriteString(lastRefresh)
	b.WriteByte('\n')
	b.WriteString("\n=== GATEWAY ===\n")

	gw, _ := data["gateway"].(map[string]any)
	if gw == nil {
		gw = map[string]any{}
	}
	b.WriteString("Status: ")
	b.WriteString(str(gw, "status"))
	b.WriteString(" | PID: ")
	b.WriteString(fmtAny(gw["pid"]))
	b.WriteString(" | Uptime: ")
	b.WriteString(str(gw, "uptime"))
	b.WriteString(" | Memory: ")
	b.WriteString(str(gw, "memory"))
	b.WriteByte('\n')

	b.WriteString("\n=== COSTS ===\n")
	b.WriteString("Today: $")
	b.WriteString(fmtCost4(flt(data, "totalCostToday")))
	b.WriteString(" (sub-agents: $")
	b.WriteString(fmtCost4(flt(data, "subagentCostToday")))
	b.WriteString(")\n")
	b.WriteString("All-time: $")
	b.WriteString(fmtCost2(flt(data, "totalCostAllTime")))
	b.WriteString(" | Projected monthly: $")
	b.WriteString(fmtCost0(flt(data, "projectedMonthly")))
	b.WriteByte('\n')

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
			b.WriteString(model)
			b.WriteString(" $")
			b.WriteString(fmtCost2(flt(m, "cost")))
		}
		b.WriteByte('\n')
	}

	sessions, _ := data["sessions"].([]any)
	sessionCount, _ := data["sessionCount"].(float64)
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
		b.WriteString(str(s, "name"))
		b.WriteString(" | ")
		b.WriteString(str(s, "model"))
		b.WriteString(" | ")
		b.WriteString(str(s, "type"))
		b.WriteString(" | context: ")
		b.WriteString(fmtPct(flt(s, "contextPct")))
		b.WriteString("%\n")
	}

	crons, _ := data["crons"].([]any)
	failed := 0
	for _, item := range crons {
		c, _ := item.(map[string]any)
		if c != nil && str(c, "lastStatus") == "error" {
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
		status := str(c, "lastStatus")
		b.WriteString("  ")
		b.WriteString(str(c, "name"))
		b.WriteString(" | ")
		b.WriteString(str(c, "schedule"))
		b.WriteString(" | ")
		b.WriteString(status)
		if status == "error" {
			b.WriteString(" ERROR: ")
			b.WriteString(str(c, "lastError"))
		}
		b.WriteByte('\n')
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
			b.WriteString("  [")
			b.WriteString(strings.ToUpper(str(a, "severity")))
			b.WriteString("] ")
			b.WriteString(str(a, "message"))
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n=== CONFIGURATION ===\n")
	ac, _ := data["agentConfig"].(map[string]any)
	if ac == nil {
		ac = map[string]any{}
	}
	b.WriteString("Primary model: ")
	b.WriteString(str(ac, "primaryModel"))
	b.WriteByte('\n')
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
	b.WriteString("Fallbacks: ")
	b.WriteString(fallbacks)
	b.WriteByte('\n')

	return b.String()
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Question string    `json:"question"`
	History  []Message `json:"history"`
}

type CompletionPayload struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
}

// gatewayError wraps a gateway failure with the appropriate HTTP status code.
// Used by handleChat to return 502 (Bad Gateway) vs 504 (Gateway Timeout).
type GatewayError struct {
	Status int // HTTP status to return to the client (502 or 504)
	Msg    string
}

func (e *GatewayError) Error() string { return e.Msg }

func CallGateway(ctx context.Context, system string, history []Message, question string, port int, token, model string, client *http.Client) (string, error) {
	// Pre-allocate messages slice: system + history + user question
	messages := make([]Message, 0, 2+len(history))
	messages = append(messages, Message{Role: "system", Content: system})
	messages = append(messages, history...)
	messages = append(messages, Message{Role: "user", Content: question})

	payload := CompletionPayload{
		Model:     model,
		Messages:  messages,
		MaxTokens: 512,
		Stream:    false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("marshal error: %v", err)}
	}

	url := "http://localhost:" + strconv.Itoa(port) + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("request error: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", &GatewayError{Status: http.StatusGatewayTimeout, Msg: "Gateway timed out — model took too long to respond"}
		}
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("gateway unreachable: %v", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayResp+1))
	if err != nil {
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("read error: %v", err)}
	}
	if len(respBody) > maxGatewayResp {
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("gateway response too large (>%d bytes)", maxGatewayResp)}
	}

	if resp.StatusCode != http.StatusOK {
		preview := string(respBody)
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("gateway HTTP %d: %s", resp.StatusCode, preview)}
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", &GatewayError{Status: http.StatusBadGateway, Msg: fmt.Sprintf("parse error: %v", err)}
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
