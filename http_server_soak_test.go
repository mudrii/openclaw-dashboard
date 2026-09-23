package dashboard

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
)

// TestNewHTTPServer_Limits locks in the Slowloris and header-size guards on
// the production http.Server.
func TestNewHTTPServer_Limits(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout <= 0 || srv.ReadHeaderTimeout > srv.ReadTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want > 0 and <= ReadTimeout %v", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
	for name, set := range map[string]bool{
		"ReadHeaderTimeout": srv.ReadHeaderTimeout == httpReadHeaderTimeout,
		"ReadTimeout":       srv.ReadTimeout == httpReadTimeout,
		"WriteTimeout":      srv.WriteTimeout == httpWriteTimeout,
		"IdleTimeout":       srv.IdleTimeout == httpIdleTimeout,
		"MaxHeaderBytes":    srv.MaxHeaderBytes == httpMaxHeaderBytes,
	} {
		if !set {
			t.Errorf("%s not set from its named constant", name)
		}
	}
	if srv.MaxHeaderBytes <= 0 || srv.MaxHeaderBytes >= http.DefaultMaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want a positive limit below the %d default", srv.MaxHeaderBytes, http.DefaultMaxHeaderBytes)
	}
}

// TestNewHTTPServer_RejectsOversizedHeaders checks the header limit is enforced
// on the wire: a request whose headers exceed it gets 431 instead of being
// buffered.
func TestNewHTTPServer_RejectsOversizedHeaders(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newHTTPServer(ln.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Serve: %v", err)
		}
	})

	get := func(headerBytes int) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Filler", strings.Repeat("a", headerBytes))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET with %d header bytes: %v", headerBytes, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(1 << 10); code != http.StatusNoContent {
		t.Errorf("small headers: status %d, want 204", code)
	}
	if code := get(2 * httpMaxHeaderBytes); code != http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("oversized headers: status %d, want 431", code)
	}
}
