package appservice

import (
	"strconv"
	"strings"
	"testing"
)

var systemdEscapeSeeds = []string{
	"127.0.0.1",
	"%h/bin",
	"$HOME/${USER}",
	"a\"b\\c",
	"line\nbreak",
	"100%%",
	"tab\tand\rcr",
	"\xff\xfe invalid utf8",
	"ünïcödé",
	"",
}

// hasUnescapedPercent reports whether s holds a '%' that is not part of a
// "%%" pair, i.e. one systemd would expand as a specifier.
func hasUnescapedPercent(s string) bool {
	return strings.Contains(strings.ReplaceAll(s, "%%", ""), "%")
}

// FuzzSystemdEscaping checks the unit-value encoders: no encoded value leaves
// a lone '%' or a raw line break, every encoding reverses to its input, and
// parseUnitBindHost recovers the --bind value from a rendered ExecStart= line.
func FuzzSystemdEscaping(f *testing.F) {
	for _, s := range systemdEscapeSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		spec := systemdEscapeSpecifiers(s)
		if hasUnescapedPercent(spec) {
			t.Fatalf("systemdEscapeSpecifiers(%q) = %q leaves a lone %%", s, spec)
		}
		if got := strings.ReplaceAll(spec, "%%", "%"); got != s {
			t.Fatalf("%%-unescape of %q = %q, want %q", spec, got, s)
		}

		quoted := systemdQuote(s)
		if strings.ContainsAny(quoted, "\r\n") || hasUnescapedPercent(quoted) {
			t.Fatalf("systemdQuote(%q) = %q has a line break or lone %%", s, quoted)
		}
		if got, err := strconv.Unquote(strings.ReplaceAll(quoted, "%%", "%")); err != nil || got != s {
			t.Fatalf("systemdQuote(%q) = %q does not round-trip: %q, %v", s, quoted, got, err)
		}

		arg := systemdExecArg(s)
		if strings.ContainsAny(arg, "\r\n") || hasUnescapedPercent(arg) {
			t.Fatalf("systemdExecArg(%q) = %q has a line break or lone %%", s, arg)
		}
		if strings.Contains(strings.ReplaceAll(arg, "$$", ""), "$") {
			t.Fatalf("systemdExecArg(%q) = %q leaves a lone $", s, arg)
		}
		unit := "[Service]\nExecStart=" + systemdExecArg("/usr/bin/dash") + " --bind " + arg + " --port 8080\n"
		if got := parseUnitBindHost(unit); got != s {
			t.Fatalf("parseUnitBindHost round-trip of %q = %q\nunit: %q", s, got, unit)
		}

		path, err := systemdPath(s)
		if strings.ContainsAny(s, "\r\n") {
			if err == nil {
				t.Fatalf("systemdPath(%q) accepted a line break", s)
			}
			return
		}
		if err != nil || hasUnescapedPercent(path) || strings.ReplaceAll(path, "%%", "%") != s {
			t.Fatalf("systemdPath(%q) = %q, %v", s, path, err)
		}
	})
}
