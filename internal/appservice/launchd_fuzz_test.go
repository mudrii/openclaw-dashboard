//go:build darwin

package appservice

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

// xmlCharOK reports whether every rune of s is a legal XML 1.0 character, the
// set xml.EscapeText preserves (others are replaced with U+FFFD).
func xmlCharOK(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		ok := r == 0x09 || r == 0x0A || r == 0x0D ||
			(r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF)
		if !ok {
			return false
		}
	}
	return true
}

// FuzzPlistRoundTrip renders the launchd plist with fuzzed values and checks
// parsePlist reads back the same host and log path, so no value can break out
// of its <string> element. parsePlist itself must never panic on the raw input.
func FuzzPlistRoundTrip(f *testing.F) {
	for _, s := range []string{
		"127.0.0.1",
		"</string><string>--port</string><string>1",
		"a & b < c > d \" '",
		"]]><!--",
		"line\nbreak",
		"\x00\x01ctl",
		"ünïcödé",
		"",
	} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, host, logPath string) {
		_ = parsePlist(host) // arbitrary bytes as a whole document

		var buf bytes.Buffer
		err := plistTmpl.Execute(&buf, plistData{
			Label:   launchdLabel,
			BinPath: "/usr/local/bin/openclaw-dashboard",
			Host:    host,
			Port:    8080,
			WorkDir: "/tmp/work",
			LogPath: logPath,
			HomeDir: "/Users/test",
			PathEnv: "/usr/bin:/bin",
		})
		if err != nil {
			t.Fatalf("render plist: %v", err)
		}
		info := parsePlist(buf.String())
		if info.Port != 8080 {
			t.Fatalf("port = %d, want 8080 (host %q, log %q)\n%s", info.Port, host, logPath, buf.String())
		}
		if xmlCharOK(host) && info.Host != strings.TrimSpace(host) {
			t.Fatalf("host round-trip %q -> %q", host, info.Host)
		}
		if xmlCharOK(logPath) && info.LogPath != strings.TrimSpace(logPath) {
			t.Fatalf("log path round-trip %q -> %q", logPath, info.LogPath)
		}
	})
}
