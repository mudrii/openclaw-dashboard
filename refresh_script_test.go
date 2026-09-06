package dashboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshScriptPreservesRuntimeSelection(t *testing.T) {
	script, err := os.ReadFile("assets/runtime/refresh.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, home, state, container string
		release, hostState           bool
	}{
		{name: "default native"},
		{name: "existing default native state", hostState: true},
		{name: "explicit home", home: "/custom/environment"},
		{name: "explicit state", state: "/custom/state"},
		{name: "container without host state", container: "selected"},
		{name: "extracted release", release: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.hostState {
				if err := os.MkdirAll(filepath.Join(dir, "home", ".openclaw"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			scriptDir := dir
			if tc.release {
				scriptDir = filepath.Join(dir, "assets", "runtime")
				if err := os.MkdirAll(scriptDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(scriptDir, "refresh.sh")
			if err := os.WriteFile(path, script, 0o755); err != nil {
				t.Fatal(err)
			}
			fake := "#!/bin/sh\nprintf 'SELECTION=%s|%s|%s|%s\\n' \"${OPENCLAW_HOME-}\" \"${OPENCLAW_STATE_DIR-}\" \"${OPENCLAW_CONTAINER-}\" \"$*\"\n"
			if err := os.WriteFile(filepath.Join(dir, "openclaw-dashboard"), []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "/bin/bash", path)
			// Selection and validation belong to the binary, including missing state.
			cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(dir, "home"), "OPENCLAW_HOME=" + tc.home, "OPENCLAW_STATE_DIR=" + tc.state, "OPENCLAW_CONTAINER=" + tc.container}
			out, err := cmd.CombinedOutput()
			want := "SELECTION=" + tc.home + "|" + tc.state + "|" + tc.container + "|--refresh"
			if err != nil || !strings.Contains(string(out), want) {
				t.Fatalf("refresh wrapper: %v, output=%s, want %s", err, out, want)
			}
		})
	}
}
