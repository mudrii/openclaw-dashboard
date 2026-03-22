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
			s.HandleStaticFile(w, r, r.URL.Path, contentType)
			return
		}
		http.NotFound(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleStaticFile serves an allowlisted file from the dashboard directory.
func (s *Server) HandleStaticFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	// Clean the path to prevent traversal
	clean := filepath.Clean(path)
	if clean != path {
		http.NotFound(w, r)
		return
	}
	fullPath := filepath.Join(s.dir, clean)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		fallbackPath := filepath.Join(s.dir, "assets", "runtime", strings.TrimPrefix(clean, "/"))
		data, err = os.ReadFile(fallbackPath)
		if err != nil {
			http.NotFound(w, r)
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
