package appsystem

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayStatusRuntimeEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, input, status, version string
	}{
		{"container RPC", `{"service":{"loaded":false,"targetRole":"diagnostic-only","runtime":{"status":"unknown","pid":3}},"gateway":{"version":"2026.8.2"},"rpc":{"ok":true,"server":{"version":"2026.8.2"}}}`, "online", "2026.8.2"},
		{"RPC denied with loaded service", `{"service":{"loaded":true},"rpc":{"ok":false}}`, "unknown", ""},
		{"RPC version precedes config version", `{"gateway":{"version":"old"},"rpc":{"ok":true,"version":"2026.8.2"}}`, "online", "2026.8.2"},
		{"not loaded text", "service not loaded", "offline", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gw := ParseGatewayStatusJSON(t.Context(), tt.input)
			if gw.Status != tt.status || gw.Version != tt.version {
				t.Fatalf("gateway = %+v; want status %q version %q", gw, tt.status, tt.version)
			}
			if gw.PID != 0 {
				t.Fatalf("non-local process metadata exposed: %+v", gw)
			}
		})
	}
}

func TestLatestVersionUsesDistTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/-/package/openclaw/dist-tags" {
			t.Errorf("requested full package metadata: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"latest":"2026.9.1","beta":"2026.10.0-beta.1"}`))
	}))
	t.Cleanup(srv.Close)
	swapSharedSystemHTTPClient(t, &http.Client{Transport: &rewriteTransport{target: srv.URL}})
	if got := FetchLatestNpmVersion(t.Context(), 1000); got != "2026.9.1" {
		t.Fatalf("latest = %q", got)
	}
}
