package appserver

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	appchat "github.com/mudrii/openclaw-dashboard/internal/appchat"
)

// handleChat handles the AI chat endpoint.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AI.Enabled {
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, errChatDisabled)
		return
	}

	// Rate limit: 10 req/min per IP (handles both IPv4 and IPv6)
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if !s.chatLimiter.allow(ip) {
		w.Header().Set("Retry-After", "60")
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
		s.sendJSONRaw(w, r, http.StatusTooManyRequests, errChatRateLimit)
		return
	}

	defer func() { _ = r.Body.Close() }()
	lr := io.LimitReader(r.Body, int64(maxBodyBytes)+1)
	bodyBytes, err := io.ReadAll(lr)
	if err != nil {
		s.sendJSONRaw(w, r, http.StatusBadRequest, errBadBody)
		return
	}
	if len(bodyBytes) > maxBodyBytes {
		s.sendJSONRaw(w, r, http.StatusRequestEntityTooLarge, errBodyTooLarge)
		return
	}

	var req appchat.Request
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		s.sendJSONRaw(w, r, http.StatusBadRequest, errBadJSON)
		return
	}

	q := strings.TrimSpace(req.Question)
	if q == "" {
		s.sendJSONRaw(w, r, http.StatusBadRequest, errEmptyQ)
		return
	}
	if utf8.RuneCountInString(q) > maxQuestionLen {
		s.sendJSONRaw(w, r, http.StatusBadRequest, errQTooLong)
		return
	}

	// Validate + sanitise history — inline switch avoids per-request map alloc
	maxHist := s.cfg.AI.MaxHistory
	history := make([]appchat.Message, 0, maxHist)
	start := max(len(req.History)-maxHist, 0)
	for _, msg := range req.History[start:] {
		switch msg.Role {
		case "user", "assistant":
		default:
			continue
		}
		content := msg.Content
		if utf8.RuneCountInString(content) > maxHistoryItem {
			// Truncate on rune boundary
			i := 0
			for j := range content {
				if i >= maxHistoryItem {
					content = content[:j]
					break
				}
				i++
			}
		}
		history = append(history, appchat.Message{Role: msg.Role, Content: content})
	}

	// Use cached data.json — avoids re-reading + parsing ~100KB per request
	dashData, err := s.GetDataCached()
	if err != nil {
		if os.IsNotExist(err) {
			s.sendJSONRaw(w, r, http.StatusServiceUnavailable, errDataMissing)
			return
		}
		s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "dashboard data is invalid"})
		return
	}

	systemPrompt := appchat.BuildSystemPrompt(dashData)
	answer, err := appchat.CallGateway(
		r.Context(), systemPrompt, history, q,
		s.cfg.AI.GatewayPort,
		s.gatewayToken,
		s.cfg.AI.Model,
		s.httpClient,
	)
	if err != nil {
		slog.Error("[dashboard] POST /api/chat error", "error", err)
		status := http.StatusBadGateway
		// Default user-facing message — never leak upstream gateway bodies which
		// may contain stack traces, model identifiers, or raw HTML 5xx pages.
		userMsg := "gateway unavailable"
		var ge *appchat.GatewayError
		if errors.As(err, &ge) {
			status = ge.Status
			if status == http.StatusGatewayTimeout {
				userMsg = "gateway timed out"
			}
		}
		s.sendJSON(w, r, status, map[string]string{"error": userMsg})
		return
	}

	slog.Info("[dashboard] POST /api/chat")
	s.sendJSON(w, r, http.StatusOK, map[string]string{"answer": answer})
}
