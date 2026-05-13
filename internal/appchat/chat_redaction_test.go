package appchat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCallGateway_NoChoicesIsError verifies the API contract: an empty
// choices array is a malformed upstream response (502), not a legitimate
// empty answer.
func TestCallGateway_NoChoicesIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	_, err := CallGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", srv.Client())
	if err == nil {
		t.Fatal("expected error when choices array is empty")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected 'no choices' in error, got %q", err.Error())
	}
	ge, ok := err.(*GatewayError)
	if !ok {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if ge.Status != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", ge.Status)
	}
}

// TestCallGateway_EmptyContentIsAllowed verifies the other half of the
// contract: a present choice with an empty content string is a valid
// (if unhelpful) answer, not an error.
func TestCallGateway_EmptyContentIsAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	answer, err := CallGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", srv.Client())
	if err != nil {
		t.Fatalf("unexpected error for empty content: %v", err)
	}
	if answer != "" {
		t.Errorf("expected empty answer, got %q", answer)
	}
}

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
