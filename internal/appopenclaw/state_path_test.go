package appopenclaw

import "testing"

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
