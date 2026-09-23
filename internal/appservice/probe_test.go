//go:build darwin || linux

package appservice

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeHTTP(t *testing.T) {
	t.Run("returns true on a 2xx response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		if !probeHTTP(srv.URL + "/") {
			t.Error("expected probeHTTP=true for 200 response")
		}
	})

	t.Run("returns true on a non-2xx response that still completes", func(t *testing.T) {
		// probeHTTP treats any completed HTTP exchange as alive; it does not
		// inspect the status code. Characterize that observed behavior.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()
		if !probeHTTP(srv.URL + "/") {
			t.Error("expected probeHTTP=true for a completed 500 response")
		}
	})

	t.Run("returns false for a closed port", func(t *testing.T) {
		// Reserve a port, then close the listener so nothing answers.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve port: %v", err)
		}
		url := "http://" + ln.Addr().String() + "/"
		if err := ln.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
		if probeHTTP(url) {
			t.Error("expected probeHTTP=false for a closed port")
		}
	})

	t.Run("returns false when the server exceeds the client timeout", func(t *testing.T) {
		// probeClient has a 2s timeout. Block past it (bounded) to exercise the
		// timeout error path without a tight wall-clock assertion.
		done := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		defer close(done) // release the handler so the server can shut down

		if probeHTTP(srv.URL + "/") {
			t.Error("expected probeHTTP=false when the response exceeds the timeout")
		}
	})
}

func TestProbeURL(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{host: "", port: 8080, want: "http://127.0.0.1:8080/"},
		{host: "0.0.0.0", port: 8080, want: "http://127.0.0.1:8080/"},
		{host: "127.0.0.1", port: 8080, want: "http://127.0.0.1:8080/"},
		{host: "::", port: 8080, want: "http://[::1]:8080/"},
		{host: "::1", port: 9090, want: "http://[::1]:9090/"},
		{host: "[::1]", port: 9090, want: "http://[::1]:9090/"},
		{host: "localhost", port: 7070, want: "http://localhost:7070/"},
		{host: "192.168.1.5", port: 7070, want: "http://192.168.1.5:7070/"},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			if got := probeURL(tc.host, tc.port); got != tc.want {
				t.Errorf("probeURL(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
			}
		})
	}
}
