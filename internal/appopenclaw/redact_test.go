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

func TestRedactExtendedFormats(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{"basic authorization", "Authorization: Basic dXNlcjpwYXNz", "Authorization: Basic [REDACTED]"},
		{"bearer authorization keeps scheme", "Authorization: Bearer abc123secret", "Authorization: Bearer [REDACTED]"},
		{"bare authorization", "authorization=abc123secret", "authorization=[REDACTED]"},
		{"escaped json token", `{\"token\":\"abc\"}`, `{\"token\":\"[REDACTED]\"}`},
		{"escaped json token with space", `{\"password\": \"a b\"}`, `{\"password\": \"[REDACTED]\"}`},
		{"quoted value with spaces", `{"password": "hunter2 with space"}`, `{"password": "[REDACTED]"}`},
		{"single quoted value with spaces", `password='hunter2 with space' ok`, `password='[REDACTED]' ok`},
		{"quoted value with escaped quote", `{"secret":"a\"b c","n":1}`, `{"secret":"[REDACTED]","n":1}`},
		{"unterminated quoted value", `password="hunter2`, `password="[REDACTED]`},
		{"token flag", "openclaw gateway call --token abc123 --json", "openclaw gateway call --token [REDACTED] --json"},
		{"api key flag", "run --api-key abc123", "run --api-key [REDACTED]"},
		{"password flag", "login --password hunter2", "login --password [REDACTED]"},
		{"prefixed token flag", "run --gateway-token abc123", "run --gateway-token [REDACTED]"},
		{"token flag equals", "run --token=abc123", "run --token=[REDACTED]"},
		{"url userinfo", "dial https://user:pw@example.com/path failed", "dial https://[REDACTED]@example.com/path failed"},
		{"url token userinfo", "git clone https://ghtoken@github.com/x/y", "git clone https://[REDACTED]@github.com/x/y"},
		{"telegram bot url", "POST https://api.telegram.org/bot123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawx/sendMessage", "POST https://api.telegram.org/[REDACTED]/sendMessage"},
		{"telegram bare token", "token 123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawx", "token [REDACTED]"},
		{"github token", "using ghp_abcdefghijklmnopqrstuvwxyz0123", "using [REDACTED]"},
		{"github server token", "using ghs_ABCDEFGHIJKLMNOPQRSTUVWXYZ", "using [REDACTED]"},
		{"slack token", "slack xoxb-123456789012-abcdefABCDEF", "slack [REDACTED]"},
		{"google api key", "key AIzaSyA1234567890abcdefghijklmnopqrstuv end", "key [REDACTED] end"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Fatalf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactLeavesOrdinaryTextUntouched(t *testing.T) {
	for _, in := range []string{
		"token usage 42",
		"tokens: 1200",
		"tokens=1200",
		"maxTokens: 512",
		"--max-tokens 512",
		"totalTokens=100 inputTokens=40",
		"secretOwners: 3",
		"passwordless login enabled",
		"GET http://localhost:18789/v1/chat/completions 200",
		"connect ws://127.0.0.1:18789 ok",
		"email user@example.com bounced",
		"at 2026-09-23T10:11:12Z pid=4242 uptime=3600",
		"sessions.list returned 12 sessions in 1234ms",
		"the bot replied in 12:30",
	} {
		t.Run(in, func(t *testing.T) {
			if got := Redact(in); got != in {
				t.Fatalf("Redact(%q) = %q, want unchanged", in, got)
			}
		})
	}
}
