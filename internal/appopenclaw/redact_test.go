package appopenclaw

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{"api secret key", "using sk-ABCD1234efgh5678 now", "using [REDACTED] now"},
		{"json api key", `{"apiKey":"xyz-123"}`, `{"apiKey":"[REDACTED]"}`},
		{"password assignment", "password=hunter2", "password=[REDACTED]"},
		{"uppercase password", "PASSWORD=Hunter2", "PASSWORD=[REDACTED]"},
		{"json access token", `{"access_token": "abc.def"}`, `{"access_token": "[REDACTED]"}`},
		{"mixed case json key", `{"ApiKey":"xyz-123"}`, `{"ApiKey":"[REDACTED]"}`},
		{"total tokens metric", "totalTokens=100", "totalTokens=100"},
		{"tokens field", "tokens: 12", "tokens: 12"},
		{"secret owners count", "secretOwners: 3", "secretOwners: 3"},
		{"plain text", "collection failed: exit status 1", "collection failed: exit status 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactBearerAuthorization(t *testing.T) {
	for _, tt := range []struct{ name, in, secret string }{
		{"bearer header", "Authorization: Bearer abc123secret", "abc123secret"},
		{"lowercase bearer", "authorization: bearer abc123secret", "abc123secret"},
		{"bare bearer", "sent Bearer abc123secret upstream", "abc123secret"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.in)
			if strings.Contains(got, tt.secret) {
				t.Fatalf("Redact(%q) = %q, still contains the secret", tt.in, got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("Redact(%q) = %q, want a redaction marker", tt.in, got)
			}
		})
	}
}
