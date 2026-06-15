package appsystem

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// portFromTestServerURL extracts the numeric port from an httptest server URL
// (e.g. "http://127.0.0.1:54321" → 54321).
func portFromTestServerURL(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse server URL %q: %v", raw, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port from %q: %v", raw, err)
	}
	return port
}

// swapSharedSystemHTTPClient replaces the package-level shared client for the
// duration of the test and restores it via t.Cleanup.
func swapSharedSystemHTTPClient(t *testing.T, c *http.Client) {
	t.Helper()
	old := sharedSystemHTTPClient
	sharedSystemHTTPClient = c
	t.Cleanup(func() { sharedSystemHTTPClient = old })
}

// TestResolveOpenclawBin verifies the PATH → asdf-shims → asdf-nodejs-installs →
// last-resort precedence. The hardcoded /usr/local and /opt/homebrew candidates
// are not injectable, so those branches are intentionally not exercised here.
func TestResolveOpenclawBin(t *testing.T) {
	writeExec := func(t *testing.T, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	t.Run("binary on PATH wins", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		binDir := t.TempDir()
		bin := filepath.Join(binDir, "openclaw")
		writeExec(t, bin)
		t.Setenv("PATH", binDir)

		if got := ResolveOpenclawBin(); got != bin {
			t.Fatalf("ResolveOpenclawBin() = %q, want PATH bin %q", got, bin)
		}
	})

	t.Run("asdf shim used when not on PATH", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PATH", "")
		shim := filepath.Join(home, ".asdf", "shims", "openclaw")
		writeExec(t, shim)

		if got := ResolveOpenclawBin(); got != shim {
			t.Fatalf("ResolveOpenclawBin() = %q, want asdf shim %q", got, shim)
		}
	})

	t.Run("asdf nodejs install picks newest version", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PATH", "")
		// No shim — only nodejs installs. Newest (20.0.0) must win over 18.0.0.
		want := filepath.Join(home, ".asdf", "installs", "nodejs", "20.0.0", "bin", "openclaw")
		for _, v := range []string{"18.0.0", "20.0.0"} {
			writeExec(t, filepath.Join(home, ".asdf", "installs", "nodejs", v, "bin", "openclaw"))
		}

		if got := ResolveOpenclawBin(); got != want {
			t.Fatalf("ResolveOpenclawBin() = %q, want newest nodejs install %q", got, want)
		}
	})

	t.Run("nothing found falls back to literal openclaw", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("PATH", "")

		if got := ResolveOpenclawBin(); got != "openclaw" {
			t.Fatalf("ResolveOpenclawBin() = %q, want literal %q", got, "openclaw")
		}
	})
}

// TestFetchLatestNpmVersion exercises the npm probe through the shared client
// seam (sharedSystemHTTPClient + rewriteTransport), covering success and every
// best-effort failure mode (all return "").
func TestFetchLatestNpmVersion(t *testing.T) {
	t.Run("200 with version", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"2026.4.11"}`))
		}))
		t.Cleanup(srv.Close)
		swapSharedSystemHTTPClient(t, &http.Client{Transport: &rewriteTransport{target: srv.URL}})

		if got := FetchLatestNpmVersion(context.Background(), 1000); got != "2026.4.11" {
			t.Fatalf("got %q, want %q", got, "2026.4.11")
		}
	})

	t.Run("malformed body returns empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"version":`))
		}))
		t.Cleanup(srv.Close)
		swapSharedSystemHTTPClient(t, &http.Client{Transport: &rewriteTransport{target: srv.URL}})

		if got := FetchLatestNpmVersion(context.Background(), 1000); got != "" {
			t.Fatalf("got %q, want empty on malformed body", got)
		}
	})

	t.Run("empty body returns empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(srv.Close)
		swapSharedSystemHTTPClient(t, &http.Client{Transport: &rewriteTransport{target: srv.URL}})

		if got := FetchLatestNpmVersion(context.Background(), 1000); got != "" {
			t.Fatalf("got %q, want empty on empty body", got)
		}
	})

	t.Run("non-200 returns empty", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"version":"2026.4.11"}`))
		}))
		t.Cleanup(srv.Close)
		swapSharedSystemHTTPClient(t, &http.Client{Transport: &rewriteTransport{target: srv.URL}})

		if got := FetchLatestNpmVersion(context.Background(), 1000); got != "" {
			t.Fatalf("got %q, want empty on non-200", got)
		}
	})

	t.Run("transport error returns empty", func(t *testing.T) {
		swapSharedSystemHTTPClient(t, &http.Client{Transport: errTransport{}})

		if got := FetchLatestNpmVersion(context.Background(), 1000); got != "" {
			t.Fatalf("got %q, want empty on transport error", got)
		}
	})
}

// errTransport always fails the round-trip, simulating a network error.
type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrServerClosed
}

// TestDetectGatewayFallback verifies the HEAD probe: a reachable server reports
// online with no Error; a closed port reports offline with Error "unreachable".
func TestDetectGatewayFallback(t *testing.T) {
	t.Run("reachable server is online", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(srv.Close)
		swapSharedSystemHTTPClient(t, srv.Client())

		gw := DetectGatewayFallback(context.Background(), portFromTestServerURL(t, srv.URL), 1000)
		if gw.Status != "online" {
			t.Fatalf("Status = %q, want online", gw.Status)
		}
		if gw.Error != nil {
			t.Fatalf("Error = %v, want nil", *gw.Error)
		}
	})

	t.Run("closed port is offline and bounded", func(t *testing.T) {
		// Start then immediately close to obtain a port nothing listens on.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		port := portFromTestServerURL(t, srv.URL)
		srv.Close()
		swapSharedSystemHTTPClient(t, srv.Client())

		start := time.Now()
		gw := DetectGatewayFallback(context.Background(), port, 50)
		elapsed := time.Since(start)

		if gw.Status != "offline" {
			t.Fatalf("Status = %q, want offline", gw.Status)
		}
		if gw.Error == nil || *gw.Error != "unreachable" {
			t.Fatalf("Error = %v, want \"unreachable\"", gw.Error)
		}
		if elapsed > time.Second {
			t.Fatalf("probe took %v, want bounded well below 1s", elapsed)
		}
	})
}

// TestProbeOpenclawGatewayEndpoints covers the success and 503 readyz branches.
func TestProbeOpenclawGatewayEndpoints(t *testing.T) {
	t.Run("healthz ok=true and readyz 503 not-ready", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz":
				_, _ = w.Write([]byte(`{"ok":true}`))
			case "/readyz":
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"ready":false,"uptimeMs":5,"failing":["x"]}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		gw, errs := probeOpenclawGatewayEndpoints(context.Background(), portFromTestServerURL(t, srv.URL), 1000)
		if len(errs) != 0 {
			t.Fatalf("errs = %v, want none", errs)
		}
		if !gw.HealthEndpointOk || !gw.Live {
			t.Fatalf("healthz: HealthEndpointOk=%v Live=%v, want both true", gw.HealthEndpointOk, gw.Live)
		}
		if !gw.ReadyEndpointOk {
			t.Fatalf("ReadyEndpointOk = false, want true (503 body still parsed)")
		}
		if gw.Ready {
			t.Fatalf("Ready = true, want false")
		}
		if gw.UptimeMs != 5 {
			t.Fatalf("UptimeMs = %d, want 5", gw.UptimeMs)
		}
		if len(gw.Failing) != 1 || gw.Failing[0] != "x" {
			t.Fatalf("Failing = %v, want [x]", gw.Failing)
		}
	})

	t.Run("healthz status=live sets Live", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz":
				_, _ = w.Write([]byte(`{"status":"live"}`))
			case "/readyz":
				_, _ = w.Write([]byte(`{"ready":true}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(srv.Close)

		gw, errs := probeOpenclawGatewayEndpoints(context.Background(), portFromTestServerURL(t, srv.URL), 1000)
		if len(errs) != 0 {
			t.Fatalf("errs = %v, want none", errs)
		}
		if !gw.Live {
			t.Fatalf("Live = false, want true (status=live)")
		}
		if !gw.Ready {
			t.Fatalf("Ready = false, want true")
		}
	})
}
