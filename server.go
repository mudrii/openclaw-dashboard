package main

import (
	"context"
	"encoding/json"
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
	"unicode/utf8"
)

const (
	maxBodyBytes   = 64 * 1024
	maxQuestionLen = 2000
	maxHistoryItem = 4000
	maxGatewayResp = 1 << 20 // 1MB limit on gateway response
	refreshTimeout = 15 * time.Second
)

// Pre-defined error JSON responses — avoid map alloc + marshal on hot paths
var (
	errChatDisabled = []byte(`{"error":"AI chat is disabled in config.json"}`)
	errBadBody      = []byte(`{"error":"failed to read body"}`)
	errBadJSON      = []byte(`{"error":"Invalid JSON body"}`)
	errEmptyQ       = []byte(`{"error":"question is required and must be non-empty"}`)
	errBodyTooLarge = []byte(`{"error":"Request body too large (max 65536 bytes)"}`)
	errQTooLong     = []byte(`{"error":"Question too long (max 2000 chars)"}`)
	errDataMissing  = []byte(`{"error":"data.json not found — refresh in progress, try again shortly"}`)
)

type Server struct {
	dir          string
	version      string
	cfg          Config
	gatewayToken string

	indexHTMLRendered  []byte
	indexContentLength string // pre-computed strconv.Itoa(len(indexHTMLRendered))
	corsDefault        string // pre-computed "http://localhost:<port>"
	httpClient         *http.Client

	mu             sync.Mutex
	lastRefresh    time.Time
	refreshRunning bool

	// Cached data.json for /api/chat prompt building
	dataMu          sync.RWMutex
	cachedData      map[string]any
	cachedDataRaw   []byte
	cachedDataMtime time.Time

	// System metrics service
	systemSvc *SystemService
}

func NewServer(dir, version string, cfg Config, gatewayToken string, indexHTML []byte, serverCtx context.Context) *Server {
	content := string(indexHTML)
	preset := html.EscapeString(cfg.Theme.Preset)
	meta := "<head>\n<meta name=\"oc-theme\" content=\"" + preset + "\">"
	content = strings.Replace(content, "<head>", meta, 1)
	content = strings.ReplaceAll(content, "__VERSION__", html.EscapeString(version))
	rendered := []byte(content)
	return &Server{
		dir:                dir,
		version:            version,
		cfg:                cfg,
		gatewayToken:       gatewayToken,
		indexHTMLRendered:  rendered,
		indexContentLength: strconv.Itoa(len(rendered)),
		corsDefault:        "http://localhost:" + strconv.Itoa(cfg.Server.Port),
		httpClient:         &http.Client{Timeout: 60 * time.Second},
		systemSvc:          NewSystemService(cfg.System, version, serverCtx),
	}
}

// PreWarm runs refresh.sh once in the background at startup so data.json
// is ready before the first browser request arrives.
func (s *Server) PreWarm() {
	go func() {
		log.Printf("[dashboard] pre-warming data.json...")
		s.runRefresh()
		log.Printf("[dashboard] pre-warm complete")
	}()
}

// allowedStatic is a whitelist of static files the Go server will serve.
// This is intentionally restrictive — Python serves everything (including
// .git/config, server.py, config.json) which is a security risk.
var allowedStatic = map[string]string{
	"/themes.json":  "application/json",
	"/favicon.ico":  "image/x-icon",
	"/favicon.png":  "image/png",
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Accept both GET and HEAD for all read endpoints
	isRead := r.Method == http.MethodGet || r.Method == http.MethodHead

	switch {
	case isRead && (r.URL.Path == "/" || r.URL.Path == "/index.html"):
		s.handleIndex(w, r)
	case isRead && r.URL.Path == "/api/system":
		s.handleSystem(w, r)
	case isRead && strings.HasPrefix(r.URL.Path, "/api/refresh"):
		s.handleRefresh(w, r)
	case r.Method == http.MethodOptions:
		s.setCORSHeaders(w, r)
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/api/chat":
		s.handleChat(w, r)
	case isRead:
		// Serve allowlisted static files from disk
		if contentType, ok := allowedStatic[r.URL.Path]; ok {
			s.handleStaticFile(w, r, r.URL.Path, contentType)
			return
		}
		http.NotFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleStaticFile serves an allowlisted file from the dashboard directory.
func (s *Server) handleStaticFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	// Clean the path to prevent traversal
	clean := filepath.Clean(path)
	if clean != path || strings.Contains(clean, "..") {
		http.NotFound(w, r)
		return
	}
	fullPath := filepath.Join(s.dir, clean)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	} else {
		w.Header().Set("Access-Control-Allow-Origin", s.corsDefault)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Length", s.indexContentLength)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(s.indexHTMLRendered)
	}
}

// runRefresh executes refresh.sh once using exec.CommandContext.
// Prevents overlapping runs. Updates lastRefresh only on success.
func (s *Server) runRefresh() {
	s.mu.Lock()
	if s.refreshRunning {
		s.mu.Unlock()
		return
	}
	s.refreshRunning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.refreshRunning = false
		s.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	script := filepath.Join(s.dir, "refresh.sh")
	cmd := exec.CommandContext(ctx, "bash", script)
	cmd.Dir = s.dir

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("[dashboard] refresh.sh timed out after %s", refreshTimeout)
		} else {
			log.Printf("[dashboard] refresh.sh failed: %v", err)
		}
		return // do NOT update lastRefresh — allow retry
	}

	s.mu.Lock()
	s.lastRefresh = time.Now()
	s.mu.Unlock()
}

// getDataRawCached returns cached data.json bytes when unchanged.
func (s *Server) getDataRawCached() ([]byte, error) {
	dataPath := filepath.Join(s.dir, "data.json")
	stat, err := os.Stat(dataPath)
	if err != nil {
		return nil, err
	}
	mtime := stat.ModTime()

	s.dataMu.RLock()
	if s.cachedDataRaw != nil && !mtime.After(s.cachedDataMtime) {
		raw := s.cachedDataRaw
		s.dataMu.RUnlock()
		return raw, nil
	}
	s.dataMu.RUnlock()

	raw, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, err
	}

	s.dataMu.Lock()
	if s.cachedDataRaw == nil || mtime.After(s.cachedDataMtime) {
		s.cachedDataRaw = raw
		s.cachedDataMtime = mtime
		s.cachedData = nil // invalidate parsed cache to prevent stale reads
	}
	s.dataMu.Unlock()
	return raw, nil
}

// handleRefresh implements stale-while-revalidate:
// Returns existing data.json immediately, triggers refresh in background if stale.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	debounce := time.Duration(s.cfg.Refresh.IntervalSeconds) * time.Second

	s.mu.Lock()
	shouldRun := !s.refreshRunning && time.Since(s.lastRefresh) >= debounce
	s.mu.Unlock()

	if shouldRun {
		go s.runRefresh()
	}

	data, err := s.getDataRawCached()
	if err != nil {
		if os.IsNotExist(err) {
			s.sendJSONRaw(w, r, http.StatusServiceUnavailable, errDataMissing)
		} else {
			s.sendJSON(w, r, http.StatusInternalServerError, map[string]string{"error": "failed to read dashboard data"})
		}
		return
	}

	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	log.Printf("[dashboard] GET /api/refresh")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// getDataCached returns parsed data.json, cached by file mtime.
// Avoids re-reading and re-parsing ~100KB JSON on every /api/chat.
func (s *Server) getDataCached() map[string]any {
	dataPath := filepath.Join(s.dir, "data.json")
	stat, err := os.Stat(dataPath)
	if err != nil {
		return map[string]any{}
	}
	mtime := stat.ModTime()

	s.dataMu.RLock()
	if s.cachedData != nil && !mtime.After(s.cachedDataMtime) {
		defer s.dataMu.RUnlock()
		return s.cachedData
	}
	s.dataMu.RUnlock()

	raw, err := os.ReadFile(dataPath)
	if err != nil {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return map[string]any{}
	}

	s.dataMu.Lock()
	// Double-check: another goroutine may have updated while we read/parsed
	if s.cachedData != nil && !mtime.After(s.cachedDataMtime) {
		defer s.dataMu.Unlock()
		return s.cachedData
	}
	s.cachedData = parsed
	s.cachedDataMtime = mtime
	s.cachedDataRaw = raw
	s.dataMu.Unlock()
	return parsed
}

// handleChat handles the AI chat endpoint.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AI.Enabled {
		s.sendJSONRaw(w, r, http.StatusServiceUnavailable, errChatDisabled)
		return
	}

	defer r.Body.Close()
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

	var req chatRequest
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
	history := make([]chatMessage, 0, maxHist)
	start := len(req.History) - maxHist
	if start < 0 {
		start = 0
	}
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
		history = append(history, chatMessage{Role: msg.Role, Content: content})
	}

	// Use cached data.json — avoids re-reading + parsing ~100KB per request
	dashData := s.getDataCached()

	systemPrompt := buildSystemPrompt(dashData)
	answer, err := callGateway(
		r.Context(), systemPrompt, history, q,
		s.cfg.AI.GatewayPort,
		s.gatewayToken,
		s.cfg.AI.Model,
		s.httpClient,
	)
	if err != nil {
		log.Printf("[dashboard] POST /api/chat error: %v", err)
		s.sendJSON(w, r, http.StatusBadGateway, map[string]string{"error": "gateway request failed"})
		return
	}

	log.Printf("[dashboard] POST /api/chat")
	s.sendJSON(w, r, http.StatusOK, map[string]string{"answer": answer})
}

// sendJSON sends a JSON response with CORS headers (for dynamic payloads).
func (s *Server) sendJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, _ := json.Marshal(v)
	s.sendJSONRaw(w, r, status, body)
}

// sendJSONRaw sends pre-encoded JSON with CORS headers (zero-alloc for known responses).
// Respects HEAD method: sends headers but no body.
func (s *Server) sendJSONRaw(w http.ResponseWriter, r *http.Request, status int, body []byte) {
	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.System.Enabled {
		body := []byte(`{"ok":false,"error":"system metrics disabled"}`)
		s.setCORSHeaders(w, r)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return
	}
	ctx := r.Context()
	status, body := s.systemSvc.GetJSON(ctx)
	s.setCORSHeaders(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
