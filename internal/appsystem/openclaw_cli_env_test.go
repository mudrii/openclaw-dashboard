package appsystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenclawCLIEnv_RemovesStaleDataDirHome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".openclaw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "openclaw.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_HOME", dir)
	t.Setenv("OPENCLAW_TEST_SENTINEL", "kept")

	env := OpenclawCLIEnv("/usr/local/bin/openclaw")
	if env == nil {
		t.Fatal("expected sanitized env")
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "OPENCLAW_HOME=") {
			t.Fatalf("OPENCLAW_HOME should be removed, got %q", kv)
		}
	}
	if !containsEnv(env, "OPENCLAW_TEST_SENTINEL=kept") {
		t.Fatalf("sanitized env should preserve unrelated variables: %v", env)
	}
}

func TestOpenclawCLIEnv_LeavesOtherCommandsAlone(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", filepath.Join(t.TempDir(), ".openclaw"))
	if env := OpenclawCLIEnv("git"); env != nil {
		t.Fatalf("non-openclaw command env = %v, want nil", env)
	}
}

func TestOpenclawCLIEnv_LeavesCustomHomeWithoutDataMarkerAlone(t *testing.T) {
	t.Setenv("OPENCLAW_HOME", filepath.Join(t.TempDir(), "custom"))
	if env := OpenclawCLIEnv("openclaw"); env != nil {
		t.Fatalf("custom home env = %v, want nil", env)
	}
}

func containsEnv(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}
