package appchat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const secretToken = "sk-test-SECRET-TOKEN-XYZ"

// TestCallGateway_ErrorRedactsToken ensures error messages built from a
// gateway response body do not leak the bearer token. A misbehaving or
// debug-mode upstream might echo the Authorization header back, and that
// payload would otherwise flow into our error string.
func TestCallGateway_ErrorRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Echo the token in the response body to simulate a leaky gateway.
		_, _ = w.Write([]byte("internal error, header was: Bearer " + secretToken))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	_, err := CallGateway(context.Background(), "sys", nil, "hi", port, secretToken, "model", srv.Client())
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("token leaked in error message: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected [redacted] marker in error %q", err.Error())
	}
}
