package appserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// allowedStatic is a whitelist of static files the server will serve.
// Intentionally restrictive to prevent leaking sensitive files.
var allowedStatic = map[string]string{
	"/themes.json": "application/json",
	"/favicon.ico": "image/x-icon",
	"/favicon.png": "image/png",
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Accept both GET and HEAD for all read endpoints
	isRead := r.Method == http.MethodGet || r.Method == http.MethodHead

	switch {
	case isRead && (r.URL.Path == "/" || r.URL.Path == "/index.html"):
		s.handleIndex(w, r)
	case isRead && r.URL.Path == "/api/system":
		s.handleSystem(w, r)
	case isRead && r.URL.Path == "/api/refresh":
		s.handleRefresh(w, r)
	case isRead && r.URL.Path == "/api/logs":
		s.handleLogs(w, r)
	case isRead && r.URL.Path == "/api/errors":
		s.handleErrors(w, r)
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
			s.HandleStaticFile(w, r, r.URL.Path, contentType)
			return
		}
		s.notFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleStaticFile serves an allowlisted file from the dashboard directory.
func (s *Server) HandleStaticFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	// Clean the path to prevent traversal
	clean := filepath.Clean(path)
	if clean != path {
		s.notFound(w, r)
		return
	}
	fullPath := filepath.Join(s.dir, clean)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		fallbackPath := filepath.Join(s.dir, "assets", "runtime", strings.TrimPrefix(clean, "/"))
		data, err = os.ReadFile(fallbackPath)
		if err != nil {
			s.notFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

// notFound sends a 404 response with CORS headers set, so browser clients
// receive a clean 404 rather than a misleading CORS error.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.setCORSHeaders(w, r)
	http.NotFound(w, r)
}

// setCORSHeaders reflects any loopback origin (any port) and defaults to the
// configured server origin otherwise. This is safe because:
//   - The dashboard binds to 127.0.0.1 by default (Server.Host), so non-loopback
//     origins cannot reach it over the network in the typical deployment.
//   - No Access-Control-Allow-Credentials header is set, so a cross-origin
//     request cannot carry cookies or HTTP auth.
//   - The /api/chat gateway token is server-side (s.gatewayToken from .env),
//     never client-supplied, and that endpoint is rate-limited to 10/min per IP.
//
// Loopback reflection exists so a developer running the SPA on a separate
// localhost port (e.g. Vite on :5173) can talk to the dashboard during
// development without disabling CORS.
func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	vary := w.Header().Get("Vary")
	switch {
	case vary == "":
		w.Header().Set("Vary", "Origin")
	case !strings.Contains(vary, "Origin"):
		w.Header().Set("Vary", vary+", Origin")
	}
	origin := r.Header.Get("Origin")
	if strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "http://[::1]:") {
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
