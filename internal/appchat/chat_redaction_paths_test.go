package appchat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRedactToken exercises the documented behavior of redactToken directly.
func TestRedactToken(t *testing.T) {
	const tok = "sk-secret"
	tests := []struct {
		name  string
		s     string
		token string
		want  string
	}{
		{"empty token leaves s unchanged", "Bearer " + tok, "", "Bearer " + tok},
		{"single occurrence redacted", "auth: " + tok, tok, "auth: [redacted]"},
		{"every occurrence redacted", tok + " and " + tok, tok, "[redacted] and [redacted]"},
		{"substring of larger word also redacted", "prefix" + tok + "suffix", tok, "prefix[redacted]suffix"},
		{"s without token verbatim", "no secrets here", tok, "no secrets here"},
		{"empty s", "", tok, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactToken(tt.s, tt.token); got != tt.want {
				t.Errorf("redactToken(%q, %q) = %q, want %q", tt.s, tt.token, got, tt.want)
			}
		})
	}
}

const redactSecret = "sk-CALLGW-SECRET-9999"

// assertGatewayErr fails if err is nil, leaks the token, or has the wrong
// status. Every CallGateway error path runs the surfaced message through
// redactToken, so the token must never appear regardless of whether the raw
// message happened to contain it. For the read/parse/unreachable paths the
// stdlib error text never echoes the body, so the token is already absent and
// this check is a no-op guard; only TestCallGateway_HTTPErrorBodyRedacted
// exercises an actual redaction substitution.
func assertGatewayErr(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), redactSecret) {
		t.Fatalf("token leaked: %q", err.Error())
	}
	var ge *GatewayError
	if !errors.As(err, &ge) {
		t.Fatalf("expected *GatewayError, got %T", err)
	}
	if ge.Status != status {
		t.Errorf("expected status %d, got %d", status, ge.Status)
	}
}

// TestCallGateway_ReadErrorClassification forces an io.ReadAll failure by
// promising a Content-Length larger than the bytes actually written, then
// hijacking and closing the connection mid-body. This exercises CallGateway's
// ERROR CLASSIFICATION, not redaction: the surfaced message is classified as a
// "read error" with status 502. The stdlib error text ("unexpected EOF") never
// echoes body bytes, so the token is already absent — the assertGatewayErr
// token check is a no-op here, asserted only as a defense-in-depth guard.
func TestCallGateway_ReadErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("ResponseWriter is not a Hijacker")
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		// Promise 100 bytes but write only a few including the secret, then
		// close abruptly so the client's body read errors out.
		fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n%s", redactSecret)
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	_, err := CallGateway(context.Background(), "sys", nil, "hi", port, redactSecret, "model", srv.Client())
	assertGatewayErr(t, err, http.StatusBadGateway)
	if !strings.Contains(err.Error(), "read error") {
		t.Fatalf("expected 'read error', got %q", err.Error())
	}
}

// TestCallGateway_ParseErrorClassification returns a 200 with a non-JSON body
// that embeds the token. This exercises ERROR CLASSIFICATION: the failure is
// classified as a "parse error" with status 502. json.Unmarshal's error reports
// the offending character/position but does not echo the body, so the token
// never reaches the message — redaction is a no-op and the token check only
// guards that the body is not accidentally surfaced.
func TestCallGateway_ParseErrorClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json " + redactSecret))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	_, err := CallGateway(context.Background(), "sys", nil, "hi", port, redactSecret, "model", srv.Client())
	assertGatewayErr(t, err, http.StatusBadGateway)
	if !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("expected 'parse error', got %q", err.Error())
	}
}

// TestCallGateway_UnreachableClassification points at a closed port. This
// exercises ERROR CLASSIFICATION: the dial failure is classified as "gateway
// unreachable" with status 502. The surfaced message carries the endpoint URL,
// not the token, so redaction is a no-op and the token check only guards that
// the message stays token-free.
func TestCallGateway_UnreachableClassification(t *testing.T) {
	// Grab a free port then close the listener so the connection is refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	_, err = CallGateway(context.Background(), "sys", nil, "hi", port, redactSecret, "model", http.DefaultClient)
	assertGatewayErr(t, err, http.StatusBadGateway)
	if !strings.Contains(err.Error(), "gateway unreachable") {
		t.Fatalf("expected 'gateway unreachable', got %q", err.Error())
	}
}

// TestCallGateway_HTTPErrorBodyRedacted is the one error path that DOES echo the
// upstream body: a non-200 response embeds a 200-char preview. When the body
// contains the token, redactToken replaces it with [redacted].
func TestCallGateway_HTTPErrorBodyRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream echoed Bearer " + redactSecret))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	_, err := CallGateway(context.Background(), "sys", nil, "hi", port, redactSecret, "model", srv.Client())
	assertGatewayErr(t, err, http.StatusBadGateway)
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("expected [redacted] marker in echoed body, got %q", err.Error())
	}
}

// TestCallGateway_OversizedBody returns more than maxGatewayResp bytes; the
// caller must reject it with a "too large" 502 (and not echo the body).
func TestCallGateway_OversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"x\":\"" + strings.Repeat("a", maxGatewayResp+10) + "\"}"))
	}))
	defer srv.Close()

	port := getPort(t, srv.URL)
	_, err := CallGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", srv.Client())
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected 'too large', got %q", err.Error())
	}
	var ge *GatewayError
	if !errors.As(err, &ge) || ge.Status != http.StatusBadGateway {
		t.Fatalf("expected 502 GatewayError, got %v", err)
	}
}

// TestCallGateway_InvalidPort covers both out-of-range bounds before any network
// activity occurs.
func TestCallGateway_InvalidPort(t *testing.T) {
	for _, port := range []int{0, -1, 65536, 70000} {
		t.Run(fmt.Sprintf("port_%d", port), func(t *testing.T) {
			_, err := CallGateway(context.Background(), "sys", nil, "hi", port, "tok", "model", http.DefaultClient)
			if err == nil {
				t.Fatal("expected error for invalid port")
			}
			if !strings.Contains(err.Error(), "invalid gateway port") {
				t.Fatalf("expected 'invalid gateway port', got %q", err.Error())
			}
			var ge *GatewayError
			if !errors.As(err, &ge) || ge.Status != http.StatusBadGateway {
				t.Fatalf("expected 502 GatewayError, got %v", err)
			}
		})
	}
}
