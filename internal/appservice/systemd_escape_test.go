package appservice

import (
	"strings"
	"testing"
)

func TestSystemdPath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain path is unquoted", in: "/tmp", want: "/tmp"},
		{name: "spaces stay literal", in: "/home/test user/dash", want: "/home/test user/dash"},
		{name: "percent is escaped", in: "/srv/100%/dash", want: "/srv/100%%/dash"},
		{name: "newline is rejected", in: "/tmp/a\nExecStartPre=/bin/evil", wantErr: true},
		{name: "carriage return is rejected", in: "/tmp/a\rb", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := systemdPath(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("systemdPath(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("systemdPath(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("systemdPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSystemdQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "PATH=/usr/bin", want: `"PATH=/usr/bin"`},
		{name: "percent escaped inside quotes", in: "OPENCLAW_HOME=/srv/50%off", want: `"OPENCLAW_HOME=/srv/50%%off"`},
		{name: "newline C-escaped", in: "a\nb", want: `"a\nb"`},
		{name: "dollar left alone", in: "X=$HOME", want: `"X=$HOME"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := systemdQuote(tc.in); got != tc.want {
				t.Errorf("systemdQuote(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestSystemdExecArg(t *testing.T) {
	got := systemdExecArg("/opt/100%/$dir/openclaw-dashboard")
	want := `"/opt/100%%/$$dir/openclaw-dashboard"`
	if got != want {
		t.Errorf("systemdExecArg = %s, want %s", got, want)
	}
}

func TestParseUnitBindHost(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "quoted ipv6 loopback", content: `ExecStart="/bin/d" --bind "::1" --port 9090`, want: "::1"},
		{name: "unquoted legacy", content: "ExecStart=/bin/d --bind 0.0.0.0 --port 9090", want: "0.0.0.0"},
		{name: "escaped percent zone", content: `ExecStart="/bin/d" --bind "fe80::1%%en0" --port 1`, want: "fe80::1%en0"},
		{name: "no bind", content: "ExecStart=/bin/d --port 9090", want: ""},
		{name: "ignores other lines", content: "Description=--bind nope\nExecStart=/bin/d --bind 127.0.0.1", want: "127.0.0.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseUnitBindHost(tc.content); got != tc.want {
				t.Errorf("parseUnitBindHost(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestSystemdEscapeRoundTripsThroughParseUnitBindHost(t *testing.T) {
	line := "ExecStart=" + systemdExecArg("/bin/d") + " --bind " + systemdExecArg("fe80::1%lo") + " --port 1"
	if !strings.Contains(line, "%%") {
		t.Fatalf("precondition: escaped line should contain %%%%: %s", line)
	}
	if got := parseUnitBindHost(line); got != "fe80::1%lo" {
		t.Errorf("round-trip host = %q, want fe80::1%%lo", got)
	}
}
