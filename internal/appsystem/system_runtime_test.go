package appsystem

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	appconfig "github.com/mudrii/openclaw-dashboard/internal/appconfig"
	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
)

// writeFakeOclawBin writes an executable shell script to a t.TempDir that emits
// the given stdout and exits with exitCode. It is the deterministic seam for
// CollectOpenclawRuntime / CollectVersionsLocal: a real subprocess on a REAL
// path (runWithTimeout shells out), portable across darwin + linux CI.
func writeFakeOclawBin(t *testing.T, stdout string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	// printf is portable; heredoc-free so quoting stays simple. The script
	// ignores its argv (status/--json/--deep/gateway) and always emits stdout.
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nexit %d\n", stdout, exitCode)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake oclaw bin: %v", err)
	}
	return bin
}

// writeSleepingOclawBin writes a fake CLI that outlives any probe deadline, so
// the caller's timeout is the only thing that can end the call.
func writeSleepingOclawBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "openclaw")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write sleeping oclaw bin: %v", err)
	}
	return bin
}

func writeArgCheckingOclawBin(t *testing.T, stdout string, exitCode int, wantArgs ...string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	check := ""
	for i, arg := range wantArgs {
		check += fmt.Sprintf("[ \"${%d}\" = %q ] || exit 64\n", i+1, arg)
	}
	check += fmt.Sprintf("[ \"$#\" -eq %d ] || exit 64\n", len(wantArgs))
	script := "#!/bin/sh\n" + check + fmt.Sprintf("printf '%%s' %q\nexit %d\n", stdout, exitCode)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write arg-checking oclaw bin: %v", err)
	}
	return bin
}

// gatewayStatusJSON is a canned `openclaw status --json` body with the INT-2
// rich blocks. eventLoop is deep-status only; the fake bin emits it regardless,
// so deepStatus=false must still parse tasks/pluginCompatibility while the lean
// caller's contract (eventLoop populated only on deep) is asserted via the
// statusArgs path — here we vary the bin output instead to isolate parsing.
const gatewayStatusLeanJSON = `{"currentVersion":"2026.5.0","tasks":{"total":3,"active":1},"pluginCompatibility":{"ok":true}}`
const gatewayStatusDeepJSON = `{"currentVersion":"2026.5.0","tasks":{"total":3,"active":1},"pluginCompatibility":{"ok":true},"eventLoop":{"degraded":false,"delayP99Ms":1.5}}`

// newGatewayHTTPTestServer returns an httptest server answering /healthz +
// /readyz, plus the numeric port it listens on (it really binds 127.0.0.1, so
// probeOpenclawGatewayEndpoints reaching http://127.0.0.1:<port> hits it).
func newGatewayHTTPTestServer(t *testing.T, healthy bool) (*httptest.Server, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ready":false}`))
			return
		}
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/readyz":
			_, _ = w.Write([]byte(`{"ready":true,"uptimeMs":99}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, portFromTestServerURL(t, srv.URL)
}

// TestCollectOpenclawRuntime exercises the INT-2 seam directly (previously only
// reachable through refresh()): deep vs lean status parsing, non-zero-exit with
// valid stdout, and independent gateway/status error accumulation.
func TestCollectOpenclawRuntime(t *testing.T) {
	// These cases assert the native gateway probe, so they must not inherit the
	// developer's OPENCLAW_CONTAINER environment.
	t.Setenv("OPENCLAW_CONTAINER", "")
	ctx := context.Background()

	t.Run("deepStatus populates tasks and eventLoop", func(t *testing.T) {
		_, port := newGatewayHTTPTestServer(t, true)
		bin := writeArgCheckingOclawBin(t, gatewayStatusDeepJSON, 0, "status", "--json", "--deep")

		oc := CollectOpenclawRuntime(ctx, bin, 1000, port, SystemVersions{}, true)

		if len(oc.Errors) != 0 {
			t.Fatalf("Errors = %v, want none", oc.Errors)
		}
		if oc.Status.Tasks == nil || oc.Status.Tasks.Total != 3 {
			t.Fatalf("Tasks = %+v, want total=3", oc.Status.Tasks)
		}
		if oc.Status.EventLoop == nil {
			t.Fatal("EventLoop = nil, want populated under deepStatus")
		}
		if oc.Status.PluginCompatibility == nil {
			t.Fatal("PluginCompatibility = nil, want populated")
		}
		if oc.Freshness.Status == "" {
			t.Fatal("Freshness.Status empty, want stamped on successful parse")
		}
		if !oc.Gateway.HealthEndpointOk || !oc.Gateway.Live {
			t.Fatalf("Gateway health=%v live=%v, want both true", oc.Gateway.HealthEndpointOk, oc.Gateway.Live)
		}
	})

	t.Run("lean status leaves eventLoop nil but keeps tasks", func(t *testing.T) {
		_, port := newGatewayHTTPTestServer(t, true)
		bin := writeArgCheckingOclawBin(t, gatewayStatusLeanJSON, 0, "status", "--json")

		oc := CollectOpenclawRuntime(ctx, bin, 1000, port, SystemVersions{}, false)

		if oc.Status.EventLoop != nil {
			t.Fatalf("EventLoop = %+v, want nil for lean status", oc.Status.EventLoop)
		}
		if oc.Status.Tasks == nil || oc.Status.Tasks.Total != 3 {
			t.Fatalf("Tasks = %+v, want total=3", oc.Status.Tasks)
		}
		if oc.Status.PluginCompatibility == nil {
			t.Fatal("PluginCompatibility = nil, want populated on lean status")
		}
	})

	t.Run("non-zero exit with valid stdout still applies parsed status", func(t *testing.T) {
		_, port := newGatewayHTTPTestServer(t, true)
		// Exit 1 (gateway connect failed) but stdout carries valid status JSON.
		bin := writeFakeOclawBin(t, gatewayStatusLeanJSON, 1)

		oc := CollectOpenclawRuntime(ctx, bin, 1000, port, SystemVersions{}, false)

		// statusFresh set ⇒ parsed status applied despite the error.
		if oc.Freshness.Status == "" {
			t.Fatal("Freshness.Status empty, want set when stdout parsed on non-zero exit")
		}
		if oc.Status.Tasks == nil || oc.Status.Tasks.Total != 3 {
			t.Fatalf("Tasks = %+v, want parsed despite non-zero exit", oc.Status.Tasks)
		}
		// The status error is still recorded independently of the applied data.
		if len(oc.Errors) == 0 {
			t.Fatal("Errors empty, want the status --json exit error recorded")
		}
	})

	t.Run("gateway and status failures accumulate independently", func(t *testing.T) {
		_, port := newGatewayHTTPTestServer(t, false) // gateway 503 → readyz/healthz errs
		// Non-JSON stdout + non-zero exit → status parse fails AND status error set.
		bin := writeFakeOclawBin(t, "boom not json", 1)

		oc := CollectOpenclawRuntime(ctx, bin, 1000, port, SystemVersions{}, false)

		if oc.Freshness.Status != "" {
			t.Fatalf("Freshness.Status = %q, want empty when parse failed", oc.Freshness.Status)
		}
		// Expect at least one gateway error (healthz 503) and the status error.
		var gatewayErr, statusErr bool
		for _, e := range oc.Errors {
			switch {
			case regexp.MustCompile(`gateway /`).MatchString(e):
				gatewayErr = true
			case regexp.MustCompile(`status --json`).MatchString(e):
				statusErr = true
			}
		}
		if !gatewayErr {
			t.Fatalf("Errors = %v, want a gateway probe error", oc.Errors)
		}
		if !statusErr {
			t.Fatalf("Errors = %v, want a status --json error", oc.Errors)
		}
	})
}

// TestGetJSON_DisabledAndColdCollect covers the two undertested GetJSON branches.
func TestGetJSON_DisabledAndColdCollect(t *testing.T) {
	t.Run("disabled returns 503 with disabled body", func(t *testing.T) {
		svc := NewSystemService(appconfig.SystemConfig{Enabled: false}, "test", context.Background())
		code, body := svc.GetJSON(context.Background())
		if code != http.StatusServiceUnavailable {
			t.Fatalf("code = %d, want 503", code)
		}
		if !regexp.MustCompile(`disabled`).Match(body) {
			t.Fatalf("body = %s, want a disabled message", body)
		}
	})

	t.Run("cold synchronous collect returns 200 with disk populated", func(t *testing.T) {
		// Fresh service, no seeded cache → GetJSON falls through to refresh().
		bin := writeFakeOclawBin(t, gatewayStatusLeanJSON, 0)
		svc := NewSystemService(appconfig.SystemConfig{
			Enabled:            true,
			PollSeconds:        10,
			MetricsTTLSeconds:  10,
			VersionsTTLSeconds: 300,
			GatewayTimeoutMs:   500,
			ColdPathTimeoutMs:  appconfig.DefaultColdPathTimeoutMs,
			CPUTimeoutMs:       appconfig.DefaultCPUTimeoutMs,
			DiskPath:           "/",
		}, "test", context.Background())
		// Pin the bin so refresh()'s openclawBin() resolves deterministically.
		svc.binOnce.Do(func() {})
		svc.binPath = bin

		code, body := svc.GetJSON(context.Background())
		if code != http.StatusOK {
			t.Fatalf("code = %d, body=%s, want 200", code, body)
		}
		// Disk on "/" via syscall.Statfs is real and always present → totalBytes>0.
		if !regexp.MustCompile(`"totalBytes":[1-9]`).Match(body) {
			t.Fatalf("body missing populated disk totalBytes: %s", body)
		}
	})
}

// TestGetProcessInfo_RealPath drives the ps path against the test's own PID,
// asserting shape (non-empty uptime, FormatBytes-shaped memory) not exact values.
func TestGetProcessInfo_RealPath(t *testing.T) {
	uptime, memory := GetProcessInfo(context.Background(), os.Getpid())
	if uptime == "" {
		t.Fatal("uptime empty, want non-empty for the running test process")
	}
	if !regexp.MustCompile(`^\d`).MatchString(memory) {
		t.Fatalf("memory = %q, want a FormatBytes-shaped value starting with a digit", memory)
	}
}

// TestCollectVersionsLocal_FallbackHTTP covers the DetectGatewayFallback HTTP
// path: the fake bin's gateway-status emits no usable JSON and exits non-zero,
// so CollectVersionsLocal falls through to the HTTP probe (routed via the shared
// client seam to a local server).
func TestCollectVersionsLocal_FallbackHTTP(t *testing.T) {
	// The fallback path is native-only; keep it native regardless of the
	// developer's OPENCLAW_CONTAINER environment.
	t.Setenv("OPENCLAW_CONTAINER", "")
	ctx := context.Background()

	t.Run("non-json gateway stdout falls back to reachable HTTP probe", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(srv.Close)
		swapSharedSystemHTTPClient(t, &http.Client{Transport: &rewriteTransport{target: srv.URL}})

		// --version succeeds; gateway status emits EMPTY stdout + non-zero exit, so
		// gw stays "unknown" (parse is skipped on empty output) and the HTTP
		// fallback fires. Non-empty non-JSON stdout would parse to "offline" via
		// the text branch and suppress the fallback — empty is the trigger.
		bin := writeFakeOclawBin(t, "", 1)
		v := CollectVersionsLocal(ctx, "dash-1.0", 500, 18789, bin)

		if v.Gateway.Status != "online" {
			t.Fatalf("Gateway.Status = %q, want online via HTTP fallback", v.Gateway.Status)
		}
	})

	t.Run("valid gateway json is parsed without fallback", func(t *testing.T) {
		// Closed-port shared client would mark fallback offline; assert parse wins.
		dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		port := portFromTestServerURL(t, dead.URL)
		dead.Close()
		swapSharedSystemHTTPClient(t, dead.Client())

		bin := writeFakeOclawBin(t,
			`{"service":{"loaded":true,"runtime":{"status":"running","pid":0}},"version":"9.9.9"}`, 0)
		v := CollectVersionsLocal(ctx, "dash-1.0", 1000, port, bin)

		if v.Gateway.Status != "online" {
			t.Fatalf("Gateway.Status = %q, want online from parsed JSON", v.Gateway.Status)
		}
		if v.Gateway.Version != "9.9.9" {
			t.Fatalf("Gateway.Version = %q, want 9.9.9 from parsed JSON", v.Gateway.Version)
		}
	})
}

func TestCollectVersionsLocal_CommandContracts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	bin := filepath.Join(dir, "openclaw")
	script := `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "--version" ]; then
	printf '%s' 'openclaw 2026.7.11'
	exit 0
fi
if [ "$#" -eq 3 ] && [ "$1" = "gateway" ] && [ "$2" = "status" ] && [ "$3" = "--json" ]; then
	printf '%s' '{"service":{"loaded":true,"runtime":{"status":"running","pid":0}},"version":"gw-1.2.3"}'
	exit 0
fi
exit 64
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}

	v := CollectVersionsLocal(ctx, "dash-1.0", 1000, 1, bin)

	if v.Openclaw != "2026.7.11" {
		t.Fatalf("Openclaw = %q, want 2026.7.11", v.Openclaw)
	}
	if v.Gateway.Status != "online" {
		t.Fatalf("Gateway.Status = %q, want online from gateway status --json", v.Gateway.Status)
	}
	if v.Gateway.Version != "gw-1.2.3" {
		t.Fatalf("Gateway.Version = %q, want gw-1.2.3", v.Gateway.Version)
	}
}

// TestParseGatewayStatusJSON_ProcessInfoAndTextFallback adds the PID-driven
// GetProcessInfo branch and the non-JSON substring fallback.
func TestParseGatewayStatusJSON_ProcessInfoAndTextFallback(t *testing.T) {
	t.Run("real pid populates uptime and memory", func(t *testing.T) {
		t.Setenv("OPENCLAW_CONTAINER", "")
		input := fmt.Sprintf(
			`{"service":{"loaded":true,"runtime":{"status":"running","pid":%d}},"version":"3.0.0"}`,
			os.Getpid())
		gw := ParseGatewayStatusJSON(context.Background(), input)
		if gw.Status != "online" {
			t.Fatalf("Status = %q, want online", gw.Status)
		}
		if gw.Uptime == "" {
			t.Fatal("Uptime empty, want populated from real pid via ps")
		}
		if !regexp.MustCompile(`^\d`).MatchString(gw.Memory) {
			t.Fatalf("Memory = %q, want FormatBytes-shaped", gw.Memory)
		}
	})

	t.Run("plain-text service loaded falls back to online", func(t *testing.T) {
		gw := ParseGatewayStatusJSON(context.Background(), "service loaded")
		if gw.Status != "online" {
			t.Fatalf("Status = %q, want online via text substring fallback", gw.Status)
		}
	})
}

// hostProbeGuardTransport fails the test if any HTTP request is issued. It is
// the seam that proves container gateway status never falls back to a
// 127.0.0.1 probe of the host.
type hostProbeGuardTransport struct{ t *testing.T }

func (h hostProbeGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	h.t.Errorf("host probe must not run in container mode: %s", req.URL)
	return nil, errors.New("host probe blocked")
}

// TestCollectVersionsLocal_ContainerSkipsHostProbe pins the container
// invariant: a loopback probe reports the host, never the container, so an
// unresolvable gateway status stays "unknown" with a reason instead of a
// plausible host-derived value.
func TestCollectVersionsLocal_ContainerSkipsHostProbe(t *testing.T) {
	for _, tt := range []struct {
		name     string
		exitCode int
		wantErr  string
		hang     bool
	}{
		{"cli succeeds with no usable json", 0, "host_probe_not_applicable", false},
		{"cli fails", 1, "unavailable", false},
		{"cli times out", 0, "timeout", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			swapSharedSystemHTTPClient(t, &http.Client{Transport: hostProbeGuardTransport{t: t}})
			ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Mode: "container", Container: "gateway"})
			bin := writeFakeOclawBin(t, "", tt.exitCode)
			if tt.hang {
				bin = writeSleepingOclawBin(t)
			}

			v := CollectVersionsLocal(ctx, "dash-1.0", 500, 18789, bin)

			if v.Gateway.Status != "unknown" {
				t.Fatalf("Gateway.Status = %q, want unknown", v.Gateway.Status)
			}
			if v.Gateway.Error == nil {
				t.Fatal("Gateway.Error = nil, want a reason code")
			}
			if *v.Gateway.Error != tt.wantErr {
				t.Fatalf("Gateway.Error = %q, want %q", *v.Gateway.Error, tt.wantErr)
			}
		})
	}
}

// TestCollectOpenclawRuntime_ContainerSkipsGatewayProbe pins that the runtime
// collector never GETs 127.0.0.1 for a container target: that probe describes
// the host gateway, so its liveness and its connection-refused errors would
// both be wrong. The gap is reported as a reason instead.
func TestCollectOpenclawRuntime_ContainerSkipsGatewayProbe(t *testing.T) {
	swapSharedSystemHTTPClient(t, &http.Client{Transport: hostProbeGuardTransport{t: t}})
	ctx := appopenclaw.WithTarget(t.Context(), appopenclaw.Target{Mode: "container", Container: "gateway"})
	bin := writeFakeOclawBin(t, gatewayStatusLeanJSON, 0)

	oc := CollectOpenclawRuntime(ctx, bin, 500, 18789, SystemVersions{}, false)

	if oc.Gateway.Reason != gatewayReasonHostProbeNotApplicable {
		t.Fatalf("Gateway.Reason = %q, want %q", oc.Gateway.Reason, gatewayReasonHostProbeNotApplicable)
	}
	if oc.Gateway.Live || oc.Gateway.Ready || oc.Gateway.HealthEndpointOk || oc.Gateway.ReadyEndpointOk {
		t.Fatalf("host gateway state leaked into the container view: %+v", oc.Gateway)
	}
	if len(oc.Errors) != 0 {
		t.Fatalf("Errors = %v, want none from a skipped probe", oc.Errors)
	}
	if oc.Freshness.Gateway != "" {
		t.Fatalf("Freshness.Gateway = %q, want empty for a probe that never ran", oc.Freshness.Gateway)
	}
	if oc.Status.CurrentVersion != "2026.5.0" {
		t.Fatalf("Status.CurrentVersion = %q, want the CLI-reported version", oc.Status.CurrentVersion)
	}
}
