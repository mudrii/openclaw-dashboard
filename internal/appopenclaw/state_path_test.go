package appopenclaw

import (
	"path/filepath"
	"testing"
)

func TestTargetStatePath(t *testing.T) {
	t.Setenv("OPENCLAW_STATE_DIR", "")
	if got := (Target{Profile: "work"}).StatePath("/home/user/.openclaw"); got != "/home/user/.openclaw-work" {
		t.Fatalf("profile state=%s", got)
	}
	if got := (Target{}).StatePath("/custom/state"); got != "/custom/state" {
		t.Fatalf("legacy state=%s", got)
	}
	t.Setenv("OPENCLAW_STATE_DIR", "/explicit/state")
	if got := (Target{Profile: "work"}).StatePath("/home/user/.openclaw"); got != "/explicit/state" {
		t.Fatalf("explicit state=%s", got)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tt := range []struct{ in, want string }{
		{"~/state", filepath.Join(home, "state")},
		{"/abs/state", "/abs/state"},
		{"~other/state", "~other/state"},
		{"", ""},
	} {
		if got := ExpandHome(tt.in); got != tt.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	t.Setenv("OPENCLAW_STATE_DIR", "~/explicit/../state")
	if got, want := (Target{}).StatePath("/fallback"), filepath.Join(home, "state"); got != want {
		t.Fatalf("tilde state=%s, want %s", got, want)
	}
}
