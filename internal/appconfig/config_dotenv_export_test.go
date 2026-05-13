package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadDotenv_ExportPrefixWithExtraSpaces verifies that the export
// keyword is stripped before key/value splitting, even when extra
// whitespace separates "export" from the variable name. Earlier
// implementation cut "export " from the trimmed key after the =-split,
// which only worked by accident; this locks in the corrected order.
func TestReadDotenv_ExportPrefixWithExtraSpaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("   export    FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadDotenv(path)
	if v, ok := m["FOO"]; !ok || v != "bar" {
		t.Errorf("expected FOO=bar with extra spaces, got map %v", m)
	}
	for k := range m {
		if k != "FOO" {
			t.Errorf("unexpected extra key %q in %v", k, m)
		}
	}
}

// TestReadDotenv_NotExportPrefixIsLiteral verifies that a key happening
// to start with "notexport" is not mistaken for an export-prefixed line.
func TestReadDotenv_NotExportPrefixIsLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("notexport=val\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := ReadDotenv(path)
	if v, ok := m["notexport"]; !ok || v != "val" {
		t.Errorf("expected notexport=val literal, got map %v", m)
	}
}
