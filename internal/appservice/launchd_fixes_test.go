//go:build darwin

package appservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLaunchd_Uninstall_NotInstalled covers the not-installed guard
// (launchd.go:191): an empty plistDir has no plist, so Uninstall reports
// "not installed" without consulting launchctl.
func TestLaunchd_Uninstall_NotInstalled(t *testing.T) {
	dir := t.TempDir()
	var called bool
	lb := &launchdBackend{
		ctx:      context.Background(),
		plistDir: dir,
		runCmd: func(context.Context, string, ...string) ([]byte, error) {
			called = true
			return nil, nil
		},
		probeFunc: func(string) bool { return true },
	}
	err := lb.Uninstall()
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("Uninstall err = %v, want 'not installed'", err)
	}
	if called {
		t.Errorf("launchctl should not be invoked when plist is absent")
	}
}

// TestLaunchd_Status_ZeroPortSkipsProbe covers the port==0 guard
// (launchd.go:245): a plist with no --port plus a valid PID must yield
// Running=false and must NOT consult the HTTP probe.
func TestLaunchd_Status_ZeroPortSkipsProbe(t *testing.T) {
	dir := t.TempDir()
	// Plist with a PID-yielding launchctl list but no --port in ProgramArguments.
	plist := `<?xml version="1.0"?>
<plist version="1.0"><dict>
  <key>Label</key><string>com.openclaw.dashboard</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/openclaw-dashboard</string>
  </array>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(dir, "com.openclaw.dashboard.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}

	var probed bool
	lb := &launchdBackend{
		ctx:      context.Background(),
		plistDir: dir,
		runCmd: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name == "launchctl" {
				return []byte(`{ "PID" = 4321; };`), nil
			}
			return nil, nil
		},
		probeFunc: func(string) bool {
			probed = true
			return true
		},
	}

	st, err := lb.Status()
	if err != nil {
		t.Fatalf("Status err = %v", err)
	}
	if st.Port != 0 {
		t.Fatalf("precondition: Port = %d, want 0", st.Port)
	}
	if st.Running {
		t.Errorf("Running = true, want false when port is 0")
	}
	if probed {
		t.Errorf("probeFunc consulted despite port==0 guard")
	}
}
