//go:build darwin

package appservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// recordingLaunchd returns a backend whose runCmd records every invocation as
// a single space-joined string. listErr controls whether `launchctl list
// <label>` reports the job as loaded (nil) or not loaded (non-nil).
func recordingLaunchd(t *testing.T, listErr error) (*launchdBackend, *[]string) {
	t.Helper()
	var calls []string
	lb := &launchdBackend{
		ctx:      context.Background(),
		plistDir: t.TempDir(),
		runCmd: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			if name == "launchctl" && len(args) > 0 && args[0] == "list" {
				return nil, listErr
			}
			return nil, nil
		},
		probeFunc: func(string) bool { return false },
	}
	return lb, &calls
}

func TestLaunchd_LifecycleCommands(t *testing.T) {
	notLoaded := errors.New("exit status 113")
	tests := []struct {
		name    string
		listErr error
		op      func(*launchdBackend) error
		want    func(plist string) []string
	}{
		{
			// KeepAlive=true makes `launchctl stop` a no-op restart; unloading
			// the job is the only way to keep it stopped.
			name: "Stop unloads the job",
			op:   (*launchdBackend).Stop,
			want: func(p string) []string { return []string{"launchctl unload " + p} },
		},
		{
			name:    "Start loads an unloaded job",
			listErr: notLoaded,
			op:      (*launchdBackend).Start,
			want: func(p string) []string {
				return []string{"launchctl list " + launchdLabel, "launchctl load " + p}
			},
		},
		{
			name: "Start kicks an already loaded job",
			op:   (*launchdBackend).Start,
			want: func(string) []string {
				return []string{"launchctl list " + launchdLabel, "launchctl start " + launchdLabel}
			},
		},
		{
			name: "Restart unloads then loads",
			op:   (*launchdBackend).Restart,
			want: func(p string) []string {
				return []string{"launchctl unload " + p, "launchctl load " + p}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lb, calls := recordingLaunchd(t, tc.listErr)
			if err := tc.op(lb); err != nil {
				t.Fatalf("op error: %v", err)
			}
			want := tc.want(lb.plistPath())
			if !slices.Equal(*calls, want) {
				t.Errorf("commands = %q, want %q", *calls, want)
			}
		})
	}
}

// withLocalZone swaps time.Local for the duration of the test.
func withLocalZone(t *testing.T, loc *time.Location) {
	t.Helper()
	orig := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })
}

func TestResolveUptime_ParsesLstartInLocalTime(t *testing.T) {
	// UTC+10 with no DST: a UTC-parse would be off by exactly ten hours.
	withLocalZone(t, time.FixedZone("TEST+10", 10*60*60))

	start := time.Now().In(time.Local).Add(-90 * time.Minute).Truncate(time.Second)
	lstart := start.Format("Mon Jan _2 15:04:05 2006")
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "ps" || !slices.Contains(args, "lstart=") {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		return []byte(lstart + "\n"), nil
	}

	got := resolveUptime(context.Background(), run, 42)
	want := 90 * time.Minute
	if diff := got - want; diff < -time.Minute || diff > time.Minute {
		t.Errorf("uptime = %v, want ~%v (lstart %q)", got, want, lstart)
	}
}

func TestResolveUptime_Failures(t *testing.T) {
	tests := []struct {
		name string
		out  string
		err  error
	}{
		{name: "ps error", err: errors.New("exit status 1")},
		{name: "empty output"},
		{name: "unparseable", out: "yesterday-ish"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := func(context.Context, string, ...string) ([]byte, error) { return []byte(tc.out), tc.err }
			if got := resolveUptime(context.Background(), run, 1); got != 0 {
				t.Errorf("uptime = %v, want 0", got)
			}
		})
	}
}

func TestLaunchd_Status_ProbesBoundHost(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{host: "::1", want: "http://[::1]:9090/"},
		{host: "0.0.0.0", want: "http://127.0.0.1:9090/"},
		{host: "127.0.0.1", want: "http://127.0.0.1:9090/"},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			dir := t.TempDir()
			plist := `<?xml version="1.0"?>
<plist version="1.0"><dict>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/openclaw-dashboard</string>
    <string>--bind</string>
    <string>` + tc.host + `</string>
    <string>--port</string>
    <string>9090</string>
  </array>
</dict></plist>`
			if err := os.WriteFile(filepath.Join(dir, launchdLabel+".plist"), []byte(plist), 0o600); err != nil {
				t.Fatal(err)
			}
			var probed string
			lb := &launchdBackend{
				ctx:      context.Background(),
				plistDir: dir,
				runCmd: func(_ context.Context, name string, _ ...string) ([]byte, error) {
					if name == "launchctl" {
						return []byte(`{ "PID" = 4321; };`), nil
					}
					return nil, errors.New("no ps")
				},
				probeFunc: func(url string) bool {
					probed = url
					return true
				},
			}
			st, err := lb.Status()
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if !st.Running {
				t.Error("Running = false, want true")
			}
			if probed != tc.want {
				t.Errorf("probed %q, want %q", probed, tc.want)
			}
		})
	}
}

func TestParsePlist_Host(t *testing.T) {
	plist := `<plist><dict><key>ProgramArguments</key><array>
<string>/bin/d</string><string>--bind</string><string>::1</string><string>--port</string><string>8080</string>
</array><key>StandardOutPath</key><string>/tmp/x.log</string></dict></plist>`
	got := parsePlist(plist)
	want := plistInfo{Port: 8080, Host: "::1", LogPath: "/tmp/x.log"}
	if got != want {
		t.Errorf("parsePlist = %+v, want %+v", got, want)
	}
}

func TestLaunchd_UninstallAfterStop(t *testing.T) {
	notLoaded := errors.New("exit status 113")
	tests := []struct {
		name      string
		listErr   error // nil: job still loaded after the failed unload
		wantErr   bool
		wantPlist bool
	}{
		{name: "stopped job (not loaded) uninstalls cleanly", listErr: notLoaded},
		{name: "unload failure on a loaded job is surfaced", wantErr: true, wantPlist: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			plist := filepath.Join(dir, launchdLabel+".plist")
			if err := os.WriteFile(plist, []byte("<plist/>"), 0o600); err != nil {
				t.Fatal(err)
			}
			lb := &launchdBackend{
				ctx:      context.Background(),
				plistDir: dir,
				runCmd: func(_ context.Context, _ string, args ...string) ([]byte, error) {
					switch args[0] {
					case "unload":
						return []byte("Could not find specified service"), errors.New("exit status 5")
					case "list":
						return nil, tc.listErr
					}
					return nil, nil
				},
				probeFunc: func(string) bool { return false },
			}
			err := lb.Uninstall()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Uninstall err = %v, wantErr %v", err, tc.wantErr)
			}
			_, statErr := os.Stat(plist)
			if exists := statErr == nil; exists != tc.wantPlist {
				t.Errorf("plist exists = %v, want %v", exists, tc.wantPlist)
			}
		})
	}
}
