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
