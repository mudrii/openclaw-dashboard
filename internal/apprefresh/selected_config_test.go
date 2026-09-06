package apprefresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

func TestSelectedConfigDoesNotMixHostAndContainer(t *testing.T) {
	host := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(host, []byte(`{"agents":{"defaults":{"model":"host/model"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, output    string
		container, fail bool
	}{
		{"native", "", false, false},
		{"container", `{"valid":true,"exists":true,"path":"/container/state/openclaw.json","config":{"agents":{"defaults":{"model":"container/model"}}}}`, true, false},
		{"container unavailable must not use host", `{}`, true, true},
		{"invalid container config must not use host", `{"valid":false,"exists":true,"config":{}}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := appopenclaw.Target{Mode: "native"}
			if tc.container {
				target = appopenclaw.Target{Mode: "container", Container: "selected"}
			}
			client := appopenclaw.Client{Runner: func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				if !tc.container {
					t.Fatal("native config called container RPC")
				}
				if appopenclaw.TargetFromContext(ctx) != target || args[2] != "config.get" {
					t.Fatalf("wrong target or method: %v %v", appopenclaw.TargetFromContext(ctx), args)
				}
				return exec.CommandContext(ctx, "printf", "%s", tc.output)
			}}
			oc, path, err := readSelectedConfig(appopenclaw.WithTarget(t.Context(), target), client, host)
			if tc.fail {
				if err == nil || oc != nil {
					t.Fatalf("unexpected fallback: %v %v", oc, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "host/model"
			if tc.container {
				want = "container/model"
				if path != "/container/state/openclaw.json" {
					t.Fatal(path)
				}
			}
			if jsonObj(jsonObj(oc, "agents"), "defaults")["model"] != want {
				t.Fatal(oc)
			}
		})
	}
}
