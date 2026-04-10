// Package appserver implements the HTTP server, routing, and request handling.
package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	appsystem "github.com/mudrii/openclaw-dashboard/internal/appsystem"
)

const (
	maxBodyBytes            = 64 * 1024
	maxQuestionLen          = 2000
	maxHistoryItem          = 4000
	refreshTimeout          = 15 * time.Second
	chatRateLimit           = 10 // max requests per minute per IP
	chatRateWindow          = 1 * time.Minute
	chatRateCleanupInterval = 5 * time.Minute
)

// Pre-defined error JSON responses — avoid map alloc + marshal on hot paths
var (
	errChatDisabled  = []byte(`{"error":"AI chat is disabled in config.json"}`)
	errBadBody       = []byte(`{"error":"failed to read body"}`)
	errBadJSON       = []byte(`{"error":"Invalid JSON body"}`)
	errEmptyQ        = []byte(`{"error":"question is required and must be non-empty"}`)
	errBodyTooLarge  = []byte(`{"error":"Request body too large (max 65536 bytes)"}`)
	errQTooLong      = []byte(`{"error":"Question too long (max 2000 chars)"}`)
	errDataMissing   = []byte(`{"error":"data.json not found — refresh in progress, try again shortly"}`)
	errChatRateLimit = []byte(`{"error":"Rate limit exceeded — max 10 requests per minute"}`)

	errInvalidDashboardData = errors.New("dashboard data is invalid")
)

// chatRateLimiter implements a simple per-IP token-bucket rate limiter for /api/chat.
// Uses sync.Map for lock-free reads on the hot path.
type chatRateLimiter struct {
	// entries maps IP → *rateBucket
	entries sync.Map
}

type rateBucket struct {
	mu        sync.Mutex
	tokens    int
	lastReset time.Time
}

// allow checks if the given IP is within rate limit. Returns true if allowed.
func (rl *chatRateLimiter) allow(ip string) bool {
	now := time.Now()
	val, _ := rl.entries.LoadOrStore(ip, &rateBucket{
		tokens:    chatRateLimit,
		lastReset: now,
	})
	bucket := val.(*rateBucket)

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	// Reset tokens if window has elapsed
	if now.Sub(bucket.lastReset) >= chatRateWindow {
		bucket.tokens = chatRateLimit
		bucket.lastReset = now
	}

	if bucket.tokens <= 0 {
		return false
	}
	bucket.tokens--
	return true
}

// cleanup removes stale entries older than 2x the rate window.
func (rl *chatRateLimiter) cleanup() {
	cutoff := time.Now().Add(-2 * chatRateWindow)
	rl.entries.Range(func(key, val any) bool {
		bucket := val.(*rateBucket)
		bucket.mu.Lock()
		stale := bucket.lastReset.Before(cutoff)
		bucket.mu.Unlock()
		if stale {
			rl.entries.Delete(key)
		}
		return true
	})
}

type Server struct {
	dir          string
	version      string
	cfg          appconfig.Config
	gatewayToken string

	indexHTMLRendered  []byte
	indexContentLength string // pre-computed strconv.Itoa(len(indexHTMLRendered))
	corsDefault        string // pre-computed "http://localhost:<port>"
	httpClient         *http.Client

	mu             sync.Mutex
	lastRefresh    time.Time
	refreshRunning bool
	refreshDone    chan struct{}

	// Cached data.json for /api/chat prompt building
	dataMu          sync.RWMutex
	cachedData      map[string]any
	cachedDataRaw   []byte
	cachedDataMtime time.Time

	// System metrics service
	systemSvc *appsystem.SystemService

	// Refresh collector function (injected at construction, not a global)
	refreshFn func(string, string, ...appconfig.Config) error

	// Chat rate limiter (10 req/min per IP)
	chatLimiter chatRateLimiter

	// Server lifecycle context — used by runRefresh to abort on shutdown.
	serverCtx context.Context
}

func NewServer(dir, version string, cfg appconfig.Config, gatewayToken string, indexHTML []byte, serverCtx context.Context, refreshFn func(string, string, ...appconfig.Config) error) *Server {
	// Defensive nil check — prevents panic if caller passes nil context.
	// Logs a warning so misconfiguration is visible; graceful-shutdown cancellation
	// will be disabled for this server instance since context.Background() never cancels.
	if serverCtx == nil {
		log.Printf("[dashboard] WARNING: NewServer called with nil serverCtx — graceful shutdown cancellation is disabled; using context.Background()")
		serverCtx = context.Background()
	}
	content := string(indexHTML)
	preset := html.EscapeString(cfg.Theme.Preset)
	meta := "<head>\n<meta name=\"oc-theme\" content=\"" + preset + "\">"
	content = strings.Replace(content, "<head>", meta, 1)
	content = strings.ReplaceAll(content, "__VERSION__", html.EscapeString(version))
	content = strings.ReplaceAll(content, "__RUNTIME__", "Go")
	rendered := []byte(content)
	s := &Server{
		dir:                dir,
		version:            version,
		cfg:                cfg,
		gatewayToken:       gatewayToken,
		indexHTMLRendered:  rendered,
		indexContentLength: strconv.Itoa(len(rendered)),
		corsDefault:        "http://localhost:" + strconv.Itoa(cfg.Server.Port),
		httpClient:         &http.Client{Timeout: 60 * time.Second},
		systemSvc:          appsystem.NewSystemService(cfg.System, version, serverCtx),
		refreshFn:          refreshFn,
		serverCtx:          serverCtx,
	}
	// Start periodic cleanup of stale rate-limit entries
	go func() {
		ticker := time.NewTicker(chatRateCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.chatLimiter.cleanup()
			case <-serverCtx.Done():
				return
			}
		}
	}()
	return s
}

// PreWarm runs refresh.sh once in the background at startup so data.json
// is ready before the first browser request arrives.
func (s *Server) PreWarm() {
	log.Printf("[dashboard] pre-warming data.json...")
	s.startRefresh()
}

func (s *Server) SystemService() *appsystem.SystemService {
	return s.systemSvc
}

// sendJSON sends a JSON response with CORS headers (for dynamic payloads).
func (s *Server) sendJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		log.Printf("[dashboard] sendJSON: json.Marshal failed: %v", err)
		body = []byte(`{"error":"internal server error"}`)
		status = http.StatusInternalServerError
	}
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
