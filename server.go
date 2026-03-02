package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxBodyBytes    = 64 * 1024
	maxQuestionLen  = 2000
	maxHistoryItem  = 4000
	refreshTimeout  = 15 * time.Second
)

type Server struct {
	dir          string
	version      string
	cfg          Config
	gatewayToken string

	indexHTMLRendered []byte
	httpClient        *http.Client

	mu          sync.Mutex
	lastRefresh time.Time
}

func NewServer(dir, version string, cfg Config, gatewayToken string, indexHTML []byte) *Server {
	content := string(indexHTML)
	preset := html.EscapeString(cfg.Theme.Preset)
	meta := fmt.Sprintf("<head>\n<meta name=\"oc-theme\" content=\"%s\">", preset)
	content = strings.Replace(content, "<head>", meta, 1)
	content = strings.ReplaceAll(content, "__VERSION__", html.EscapeString(version))
	return &Server{
		dir:               dir,
		version:           version,
		cfg:               cfg,
		gatewayToken:      gatewayToken,
		indexHTMLRendered: []byte(content),
		httpClient:        &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html"):
		s.handleIndex(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/refresh"):
		s.handleRefresh(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/chat":
		s.handleChat(w, r)
	case r.Method == http.MethodGet:
		http.NotFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	} else {
		w.Header().Set("Access-Control-Allow-Origin", fmt.Sprintf("http://localhost:%d", s.cfg.Server.Port))
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Length", strconv.Itoa(len(s.indexHTMLRendered)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.indexHTMLRendered)
}

// handleRefresh runs refresh.sh (debounced) and returns data.json.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	debounce := time.Duration(s.cfg.Refresh.IntervalSeconds) * time.Second
	if debounce == 0 {
		debounce = 30 * time.Second
	}

	s.mu.Lock()
	shouldRun := time.Since(s.lastRefresh) >= debounce
	if shouldRun {
		s.lastRefresh = time.Now() // mark early to prevent parallel runs
	}
	s.mu.Unlock()

	if shouldRun {
		script := filepath.Join(s.dir, "refresh.sh")
		cmd := exec.Command("bash", script)
		cmd.Dir = s.dir
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case err := <-done:
			if err != nil {
				log.Printf("[dashboard] refresh.sh failed: %v", err)
			}
		case <-time.After(refreshTimeout):
			_ = cmd.Process.Kill()
			log.Printf("[dashboard] refresh.sh timed out after %s", refreshTimeout)
		}
	}

	dataPath := filepath.Join(s.dir, "data.json")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.sendJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": "data.json not found"})
		} else {
			s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	log.Printf("[dashboard] GET /api/refresh")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleChat handles the AI chat endpoint.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AI.Enabled {
		s.sendJSON(w, r, http.StatusServiceUnavailable, map[string]string{"error": "AI chat is disabled in config.json"})
		return
	}

	// Body size check
	lr := io.LimitReader(r.Body, int64(maxBodyBytes)+1)
	bodyBytes, err := io.ReadAll(lr)
	if err != nil {
		s.sendJSON(w, r, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	if len(bodyBytes) > maxBodyBytes {
		s.sendJSON(w, r, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("Request body too large (max %d bytes)", maxBodyBytes),
		})
		return
	}

	var req chatRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		s.sendJSON(w, r, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	q := strings.TrimSpace(req.Question)
	if q == "" {
		s.sendJSON(w, r, http.StatusBadRequest, map[string]string{"error": "question is required and must be non-empty"})
		return
	}
	if len(q) > maxQuestionLen {
		s.sendJSON(w, r, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Question too long (max %d chars)", maxQuestionLen),
		})
		return
	}

	// Validate + sanitise history
	maxHist := s.cfg.AI.MaxHistory
	allowed := map[string]bool{"user": true, "assistant": true}
	var history []chatMessage
	start := len(req.History) - maxHist
	if start < 0 {
		start = 0
	}
	for _, msg := range req.History[start:] {
		if !allowed[msg.Role] {
			continue
		}
		content := msg.Content
		if len(content) > maxHistoryItem {
			content = content[:maxHistoryItem]
		}
		history = append(history, chatMessage{Role: msg.Role, Content: content})
	}

	// Load data.json for system prompt
	var dashData map[string]any
	dataPath := filepath.Join(s.dir, "data.json")
	if raw, err := os.ReadFile(dataPath); err == nil {
		_ = json.Unmarshal(raw, &dashData)
	}
	if dashData == nil {
		dashData = map[string]any{}
	}

	systemPrompt := buildSystemPrompt(dashData)
	answer, err := callGateway(
		systemPrompt, history, q,
		s.cfg.AI.GatewayPort,
		s.gatewayToken,
		s.cfg.AI.Model,
		s.httpClient,
	)
	if err != nil {
		log.Printf("[dashboard] POST /api/chat error: %v", err)
		s.sendJSON(w, r, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}

	log.Printf("[dashboard] POST /api/chat")
	s.sendJSON(w, r, http.StatusOK, map[string]string{"answer": answer})
}

// sendJSON sends a JSON response with CORS headers.
func (s *Server) sendJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, _ := json.Marshal(v)
	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
