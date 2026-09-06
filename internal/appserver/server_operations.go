package appserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

type operationRequest struct {
	OperationID    string `json:"operationId"`
	Action         string `json:"action"`
	ID             string `json:"id,omitempty"`
	ConfigRevision string `json:"configRevision,omitempty"`
	SessionKey     string `json:"sessionKey,omitempty"`
	RunID          string `json:"runId,omitempty"`
}

// maxOperationAuditFiles bounds the operation audit directory. Reservations are
// kept newest-first: the identity of a long-past operation cannot be replayed
// by a client that no longer remembers it, so old records are safe to drop.
const maxOperationAuditFiles = 500

var operationIDPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func localOperationRequest(r *http.Request) bool {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || u.Host != r.Host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return false
		}
	}
	return true
}

func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.cfg.Operations.Enabled || len(s.operatorToken) < 32 {
		s.sendJSON(w, r, http.StatusForbidden, map[string]string{"error": "operations_disabled"})
		return
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !localOperationRequest(r) || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(provided), []byte(s.operatorToken)) != 1 {
		s.sendJSON(w, r, http.StatusForbidden, map[string]string{"error": "operator_authorization_required"})
		return
	}
	if !s.operationLimiter.allow("operator") {
		s.sendJSON(w, r, 429, map[string]string{"error": "rate_limited"})
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		s.sendJSON(w, r, 415, map[string]string{"error": "json_required"})
		return
	}
	var request operationRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	err := d.Decode(&request)
	var extra any
	if err != nil || d.Decode(&extra) != io.EOF || !operationIDPattern.MatchString(request.OperationID) {
		s.sendJSON(w, r, 400, map[string]string{"error": "invalid_operation"})
		return
	}
	switch request.Action {
	case "automation.enable", "automation.disable", "automation.run":
		if !validRuntimeKey(request.ID) || strings.ContainsAny(request.ID, `/\\`) || !validRuntimeKey(request.ConfigRevision) {
			s.sendJSON(w, r, 400, map[string]string{"error": "exact_automation_revision_required"})
			return
		}
	case "session.abort":
		if !validRuntimeKey(request.SessionKey) || !validRuntimeKey(request.RunID) {
			s.sendJSON(w, r, 400, map[string]string{"error": "exact_session_run_required"})
			return
		}
	default:
		s.sendJSON(w, r, 400, map[string]string{"error": "unsupported_operation"})
		return
	}
	// Reserve the identity durably before contacting the gateway. An interrupted
	// request stays reserved: retrying a mutation after a lost reply is unsafe.
	auditDir := filepath.Join(s.dir, "operations")
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		s.sendJSON(w, r, 500, map[string]string{"error": "audit_unavailable"})
		return
	}
	// #nosec G703 -- directory is trusted server configuration and filename is a validated UUID.
	audit, err := os.OpenFile(filepath.Join(auditDir, request.OperationID+".jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		code := 500
		if errors.Is(err, os.ErrExist) {
			code = 409
		}
		s.sendJSON(w, r, code, map[string]string{"error": "operation_identity_unavailable"})
		return
	}
	defer func() { _ = audit.Close() }()
	record := map[string]any{"operationId": request.OperationID, "action": request.Action, "id": request.ID, "sessionKey": request.SessionKey, "runId": request.RunID, "target": s.cfg.Openclaw.Effective(), "requestedAt": time.Now().UTC(), "outcome": "unknown"}
	if err := json.NewEncoder(audit).Encode(record); err != nil || audit.Sync() != nil {
		s.sendJSON(w, r, 500, map[string]string{"error": "audit_unavailable"})
		return
	}
	// Persist the reservation's directory entry as well as its contents.
	directory, err := os.Open(auditDir)
	if err != nil {
		s.sendJSON(w, r, 500, map[string]string{"error": "audit_unavailable"})
		return
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		s.sendJSON(w, r, 500, map[string]string{"error": "audit_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(appopenclaw.WithTarget(r.Context(), s.cfg.Openclaw), 35*time.Second)
	defer cancel()
	var result map[string]any
	var operationErr error
	conflict := false
	if request.Action == "session.abort" {
		var known bool
		known, operationErr = apprefresh.HasExactActiveRun(ctx, s.runtimeClient, request.SessionKey, request.RunID)
		conflict = operationErr == nil && !known
	} else {
		var job map[string]any
		operationErr = s.runtimeClient.Read(ctx, "cron.get", map[string]string{"id": request.ID}, &job)
		conflict = operationErr == nil && job["configRevision"] != request.ConfigRevision
		if request.Action == "automation.run" && job["enabled"] != true {
			conflict = true
		}
	}
	outcome, status := "not_applied", http.StatusConflict
	if operationErr == nil && !conflict {
		switch request.Action {
		case "automation.enable", "automation.disable":
			operationErr = s.runtimeClient.SetAutomationEnabled(ctx, request.ID, request.ConfigRevision, request.Action == "automation.enable", &result)
		case "automation.run":
			operationErr = s.runtimeClient.RunAutomation(ctx, request.ID, &result)
		case "session.abort":
			operationErr = s.runtimeClient.AbortRun(ctx, request.SessionKey, request.RunID, &result)
		}
		outcome, status = "applied", http.StatusOK
		if operationErr != nil {
			outcome = "unknown"
		} // execution might precede a lost response
		if result["ran"] == false || result["aborted"] == false {
			outcome = "not_applied"
		}
	}
	code := appopenclaw.ErrorCode(operationErr)
	if operationErr != nil {
		status = http.StatusBadGateway
		if code == "permission_denied" {
			status = http.StatusForbidden
		}
		if code == "unsupported" {
			status = http.StatusNotImplemented
		}
	}
	if conflict {
		code = "target_changed_or_inactive"
	}
	record["outcome"], record["errorCode"], record["finishedAt"] = outcome, code, time.Now().UTC()
	// Append, rather than overwrite, so the pre-execution reservation survives a crash.
	if err := json.NewEncoder(audit).Encode(record); err != nil || audit.Sync() != nil {
		outcome, status, code = "unknown", 500, "audit_unavailable"
	}
	pruneOperationAudit(auditDir)
	s.sendJSON(w, r, status, map[string]any{"operationId": request.OperationID, "outcome": outcome, "errorCode": code})
}

// pruneOperationAudit deletes the oldest records once the directory exceeds
// maxOperationAuditFiles. Retention is best effort: a failure to prune is
// logged for the operator but never fails the operation it followed.
func pruneOperationAudit(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("[dashboard] operation audit pruning skipped", "error", err)
		return
	}
	if len(entries) <= maxOperationAuditFiles {
		return
	}
	type record struct {
		name     string
		modified time.Time
	}
	records := make([]record, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			// A concurrent pruner already removed this record: it is one fewer
			// file to delete, not a reason to abandon the pass.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			slog.Warn("[dashboard] operation audit pruning skipped", "error", err)
			return
		}
		records = append(records, record{name: entry.Name(), modified: info.ModTime()})
	}
	if len(records) <= maxOperationAuditFiles {
		return
	}
	// Name breaks mtime ties so the same records are chosen on every pass.
	slices.SortFunc(records, func(a, b record) int {
		if c := a.modified.Compare(b.modified); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	for _, stale := range records[:len(records)-maxOperationAuditFiles] {
		if err := os.Remove(filepath.Join(dir, stale.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("[dashboard] operation audit pruning failed", "error", err)
			return
		}
	}
}
