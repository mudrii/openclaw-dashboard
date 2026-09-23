package appsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

// sharedSystemHTTPClient is reused across all system probes (gateway healthz/
// readyz, npm version). Every call site already wraps requests in a per-call
// context deadline (1.5–3s); the client-level Timeout is a defense-in-depth
// backstop, set well above any per-call deadline so it never interferes yet
// still bounds a future caller that forgets to pass a deadlined context.
var sharedSystemHTTPClient = &http.Client{Timeout: 30 * time.Second}

// maxJSONResponseBytes caps every JSON body we decode from the gateway or npm
// dist-tags endpoint. Keep full package metadata out of this small-body path.
const maxJSONResponseBytes = 1 << 16

func probeOpenclawGatewayEndpoints(ctx context.Context, gatewayPort int, timeoutMs int) (SystemOpenclawGateway, []string) {
	if gatewayPort <= 0 {
		gatewayPort = defaultGatewayPort
	}
	if timeoutMs <= 0 {
		timeoutMs = defaultGatewayProbeTimeoutMs
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	base := fmt.Sprintf("http://127.0.0.1:%d", gatewayPort)
	client := sharedSystemHTTPClient
	gw := SystemOpenclawGateway{}
	var errs []string

	if m, err := FetchJSONMap(tctx, client, base+"/healthz"); err != nil {
		errs = append(errs, "gateway /healthz: "+err.Error())
	} else {
		gw.HealthEndpointOk = true
		if ok, okSet := BoolFromAny(m["ok"]); okSet {
			gw.Live = ok
		}
		if s, ok := m["status"].(string); ok && strings.EqualFold(s, "live") {
			gw.Live = true
		}
	}

	// readyz returns 503 when not ready — but the body still contains useful JSON
	// (ready, failing, uptimeMs). Parse it on both 200 and 503.
	if m, err := fetchJSONMapAllowStatus(tctx, client, base+"/readyz", 200, 503); err != nil {
		errs = append(errs, "gateway /readyz: "+err.Error())
	} else {
		gw.ReadyEndpointOk = true
		if ready, ok := BoolFromAny(m["ready"]); ok {
			gw.Ready = ready
		}
		if uptime, ok := int64FromAny(m["uptimeMs"]); ok {
			gw.UptimeMs = uptime
		}
		gw.Failing = stringSliceFromAny(m["failing"])
	}

	return gw, errs
}

// fetchJSONMapAllowStatus is like fetchJSONMap but accepts specific HTTP status
// codes as valid (e.g., readyz returns 503 with a useful JSON body).
func fetchJSONMapAllowStatus(ctx context.Context, client *http.Client, url string, allowedStatuses ...int) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	allowed := slices.Contains(allowedStatuses, resp.StatusCode)
	if !allowed {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// FetchJSONMap GETs url and decodes a JSON object body (capped at
// maxJSONResponseBytes), rejecting any non-2xx status.
func FetchJSONMap(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// Reject any non-2xx status — both 4xx (client error) and 5xx (server error)
	// indicate the endpoint did not return a valid JSON payload we should trust. (I1 fix)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// DetectGatewayFallback checks if the gateway HTTP port is responding.
// timeoutMs controls how long to wait; defaults to 1500ms if <= 0.
func DetectGatewayFallback(ctx context.Context, gatewayPort int, timeoutMs int) SystemGateway {
	if gatewayPort <= 0 {
		gatewayPort = defaultGatewayPort
	}
	if timeoutMs <= 0 {
		timeoutMs = defaultGatewayProbeTimeoutMs
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodHead, fmt.Sprintf("http://127.0.0.1:%d/", gatewayPort), nil)
	if err != nil {
		e := "probe failed"
		return SystemGateway{Status: "offline", Error: &e}
	}
	client := sharedSystemHTTPClient
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		return SystemGateway{Status: "online"}
	}
	e := "unreachable"
	return SystemGateway{Status: "offline", Error: &e}
}

// FetchLatestNpmVersion queries the npm registry for the latest openclaw version.
// Best-effort: returns "" on any error.
func FetchLatestNpmVersion(ctx context.Context, timeoutMs int) string {
	if timeoutMs <= 0 {
		timeoutMs = npmLatestTimeoutMs
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := sharedSystemHTTPClient
	req, err := http.NewRequestWithContext(tctx, http.MethodGet, "https://registry.npmjs.org/-/package/openclaw/dist-tags", nil)
	if err != nil {
		slog.Warn("[dashboard] FetchLatestNpmVersion: request creation failed", "error", err)
		return ""
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("[dashboard] FetchLatestNpmVersion: request failed", "error", err)
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("[dashboard] FetchLatestNpmVersion: unexpected status", "status", resp.StatusCode)
		return ""
	}
	var pkg struct {
		Version string `json:"latest"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&pkg); err != nil {
		slog.Warn("[dashboard] FetchLatestNpmVersion: JSON decode failed", "error", err)
		return ""
	}
	return pkg.Version
}
