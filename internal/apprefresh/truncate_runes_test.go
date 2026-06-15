package apprefresh

import (
	"testing"
	"unicode/utf8"
)

// TestTruncateRunes proves truncation cuts on a rune boundary so the result is
// always valid UTF-8 — a byte-slice cut (s[:n]) could split a multibyte rune and
// emit U+FFFD into data.json.
func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"ascii under limit", "hello", 10, "hello"},
		{"ascii exact", "hello", 5, "hello"},
		{"ascii over limit", "hello world", 5, "hello"},
		{"multibyte not split at boundary", "héllo", 3, "hél"},
		{"emoji boundary kept whole", "ab😀cd", 3, "ab😀"},
		{"cut just before multibyte rune", "a😀b", 1, "a"},
		{"zero keeps nothing", "abc", 0, ""},
		{"negative treated as zero", "abc", -1, ""},
		{"empty string", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateRunes(tt.in, tt.n)
			if got != tt.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}

// TestTruncateBytes proves the byte-cap backs up to a rune boundary so the
// result never exceeds maxBytes AND is always valid UTF-8 (a raw s[:maxBytes]
// could both overshoot intent and split a rune).
func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		maxBytes int
		want     string
	}{
		{"ascii under", "hello", 10, "hello"},
		{"ascii exact", "hello", 5, "hello"},
		{"ascii over", "hello world", 5, "hello"},
		{"cut lands mid 2-byte rune backs up", "abé", 3, "ab"}, // é is bytes 2-3; cap 3 splits it → back to "ab"
		{"cut lands on rune start keeps it", "abé", 4, "abé"},
		{"emoji split backs up to before it", "ab\U0001F600", 4, "ab"}, // 😀 is 4 bytes at offset 2; cap 4 splits → "ab"
		{"zero", "abc", 0, ""},
		{"negative", "abc", -1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateBytes(tt.in, tt.maxBytes)
			if got != tt.want {
				t.Errorf("truncateBytes(%q, %d) = %q, want %q", tt.in, tt.maxBytes, got, tt.want)
			}
			if len(got) > tt.maxBytes && tt.maxBytes >= 0 {
				t.Errorf("result %q exceeds maxBytes %d", got, tt.maxBytes)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}
