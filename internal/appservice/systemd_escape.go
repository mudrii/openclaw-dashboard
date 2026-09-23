package appservice

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// systemd unit-file value encoding. These helpers are platform-neutral so the
// escaping rules are unit-tested on every host, not only on Linux.

// systemdEscapeSpecifiers doubles every '%' so systemd does not expand it as
// a unit specifier (%h, %u, ...). Applies to every templated unit value.
func systemdEscapeSpecifiers(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

// systemdQuote renders s as a double-quoted, C-escaped value for settings that
// unquote their argument (Environment=, ExecStart=), with specifiers escaped.
func systemdQuote(s string) string {
	return systemdEscapeSpecifiers(strconv.Quote(s))
}

// systemdExecArg renders one ExecStart= argument. On top of systemdQuote it
// doubles '$', because ExecStart= expands $VAR and ${VAR} references.
func systemdExecArg(s string) string {
	return strings.ReplaceAll(systemdQuote(s), "$", "$$")
}

// systemdPath renders a path for settings that take the raw remainder of the
// line (WorkingDirectory=). systemd does not unquote these, so the path is
// written verbatim apart from specifier escaping. Line breaks cannot be
// represented and would inject extra directives, so they are rejected.
func systemdPath(p string) (string, error) {
	if strings.ContainsAny(p, "\r\n") {
		return "", errors.New("path contains a line break")
	}
	return systemdEscapeSpecifiers(p), nil
}

var unitBindRe = regexp.MustCompile(`(?:^|\s)--bind\s+("(?:[^"\\]|\\.)*"|\S+)`)

// parseUnitBindHost extracts the --bind value from the ExecStart= line of a
// rendered unit, reversing systemdExecArg's quoting and escaping. Returns ""
// when absent.
func parseUnitBindHost(content string) string {
	for line := range strings.Lines(content) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		match := unitBindRe.FindStringSubmatch(line)
		if len(match) != 2 {
			return ""
		}
		host := match[1]
		if unquoted, err := strconv.Unquote(host); err == nil {
			host = unquoted
		}
		host = strings.ReplaceAll(host, "$$", "$")
		return strings.ReplaceAll(host, "%%", "%")
	}
	return ""
}
