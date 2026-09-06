package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mudrii/openclaw-dashboard/internal/appchat"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

func (s *Server) handleWorkboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(appopenclaw.WithTarget(r.Context(), s.cfg.Openclaw), 25*time.Second)
	defer cancel()
	result, err := apprefresh.ReadWorkboardSummary(ctx, s.runtimeClient)
	if err != nil {
		s.runtimeReadError(w, r, err)
		return
	}
	s.sendJSON(w, r, http.StatusOK, result)
}

func validRuntimeKey(key string) bool {
	return key != "" && len(key) <= 512 && strings.IndexFunc(key, unicode.IsControl) < 0
}

func (s *Server) handleChatCapability(w http.ResponseWriter, r *http.Request) {
	s.sendJSON(w, r, http.StatusOK, s.chatCapability(r.Context()))
}

func (s *Server) chatCapability(ctx context.Context) appchat.Capability {
	if !s.cfg.Openclaw.IsContainer() {
		return appchat.CheckCapability(s.cfg.AI.Enabled, s.openclawPath, s.gatewayToken)
	}
	var snapshot struct {
		Config json.RawMessage `json:"config"`
		Valid  bool            `json:"valid"`
		Exists bool            `json:"exists"`
	}
	if s.cfg.AI.Enabled {
		ctx = appopenclaw.WithTarget(ctx, s.cfg.Openclaw)
		if err := s.runtimeClient.Read(ctx, "config.get", map[string]any{}, &snapshot); err != nil || !snapshot.Valid || !snapshot.Exists {
			snapshot.Config = nil
		}
	}
	return appchat.CheckCapabilityJSON(s.cfg.AI.Enabled, snapshot.Config, s.gatewayToken)
}

func (s *Server) runtimeReadError(w http.ResponseWriter, r *http.Request, err error) {
	code := appopenclaw.ErrorCode(err)
	status := http.StatusBadGateway
	if code == "permission_denied" {
		status = http.StatusForbidden
	}
	if code == "unsupported" {
		status = http.StatusNotImplemented
	}
	s.sendJSON(w, r, status, map[string]any{"state": "unavailable", "errorCode": code})
}

func (s *Server) handleAutomationRuns(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	offset := 0
	var err error
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
	}
	if !validRuntimeKey(id) || strings.ContainsAny(id, `/\\`) || err != nil || offset < 0 || offset > 100000 {
		s.sendJSON(w, r, http.StatusBadRequest, map[string]string{"error": "invalid automation history request"})
		return
	}
	ctx := appopenclaw.WithTarget(r.Context(), s.cfg.Openclaw)
	result, err := apprefresh.ReadAutomationRuns(ctx, s.runtimeClient, id, offset, 50)
	if err != nil {
		s.runtimeReadError(w, r, err)
		return
	}
	s.sendJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleSessionWork(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if !validRuntimeKey(key) {
		s.sendJSON(w, r, http.StatusBadRequest, map[string]string{"error": "invalid session key"})
		return
	}
	ctx := appopenclaw.WithTarget(r.Context(), s.cfg.Openclaw)
	s.sendJSON(w, r, http.StatusOK, apprefresh.ReadSessionWork(ctx, s.runtimeClient, key))
}
