//go:build darwin

package appservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTailFile_invalidUTF8Sanitized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tail.log")
	// Three lines: valid, invalid mid-line, valid. \xff is never a UTF-8 byte.
	content := []byte("alpha\nbet\xffa\ngamma\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write tail file: %v", err)
	}
	lines := tailFile(path, 10)
	if len(lines) == 0 {
		t.Fatal("tailFile returned no lines")
	}
	for _, line := range lines {
		if !utf8.ValidString(line) {
			t.Errorf("line is not valid UTF-8: %q", line)
		}
	}
	// Either the bad line was dropped or sanitized. Valid lines must remain.
	got := strings.Join(lines, "|")
	if !strings.Contains(got, "alpha") {
		t.Errorf("expected 'alpha' in tail output, got: %q", got)
	}
	if !strings.Contains(got, "gamma") {
		t.Errorf("expected 'gamma' in tail output, got: %q", got)
	}
}
